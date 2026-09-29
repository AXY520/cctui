package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"cctui/internal/ccswitch"
	"cctui/internal/update"
)

type screenMode int

const (
	modeList screenMode = iota
	modeForm
	modeConfirm
	modeModelPicker
	modeUpdateConfirm
)

type rowKind int

const (
	rowHeading rowKind = iota
	rowSpacer
	rowProvider
	rowAdd
)

type statusLevel int

const (
	statusInfo statusLevel = iota
	statusSuccess
	statusError
)

type listRow struct {
	kind     rowKind
	app      ccswitch.AppType
	provider *ccswitch.Provider
	key      string
}

type formState struct {
	app             ccswitch.AppType
	editMode        bool
	original        *ccswitch.Provider
	fields          []textinput.Model
	labels          []string
	focusIndex      int
	errorMessage    string
	modelFieldIndex int
}

type modelPickerState struct {
	app         ccswitch.AppType
	title       string
	models      []string
	cursor      int
	targetField int
	filter      string
	filtering   bool
}

// visible 返回按过滤词筛选后的候选列表。
func (p *modelPickerState) visible() []string {
	return fuzzyFilter(p.models, p.filter)
}

type confirmState struct {
	app      ccswitch.AppType
	provider ccswitch.Provider
}

type modelFetchResultMsg struct {
	app    ccswitch.AppType
	models []string
	err    error
}

type pingResultMsg struct {
	providerID string
	app        ccswitch.AppType
	latency    time.Duration
	statusCode int
	err        error
}

type updateCheckResultMsg struct {
	info   *update.Info
	err    error
	manual bool
}

type updateApplyResultMsg struct {
	path    string
	version string
	err     error
}

var Version = "dev"

var reasoningEffortOptions = []string{
	"", // 默认/不设置
	"minimal",
	"low",
	"medium",
	"high",
	"xhigh",
}

var reasoningSummaryOptions = []string{
	"",
	"auto",
	"concise",
	"detailed",
	"none",
}

var modelVerbosityOptions = []string{
	"",
	"low",
	"medium",
	"high",
}

var serviceTierOptions = []string{
	"",
	"default",
	"priority",
	"flex",
	"fast",
}

var wireAPIOptions = []string{
	"responses",
}

var piAPITypeOptions = []string{
	"openai-completions",
	"openai-responses",
	"anthropic-messages",
	"google-generative-ai",
}

var piReasoningOptions = []string{
	"true",
	"false",
}

var triStateOptions = []string{
	"",
	"true",
	"false",
}

var maxTokensFieldOptions = []string{
	"",
	"max_tokens",
	"max_completion_tokens",
}

// selectFieldsWithDefault 空值在 UI 显示为「默认」
var selectFieldsWithDefault = map[string]bool{
	"Reasoning Effort":            true,
	"Reasoning Summary":           true,
	"Verbosity":                   true,
	"Service Tier":                true,
	"Max Tokens Field":            true,
	"Supports Developer Role":     true,
	"Supports Reasoning Effort":   true,
	"Supports Usage In Streaming": true,
}

type Model struct {
	store          *ccswitch.Store
	width          int
	height         int
	mode           screenMode
	rows           []listRow
	cursor         int
	current        map[ccswitch.AppType]string
	providers      map[ccswitch.AppType][]ccswitch.Provider
	form           formState
	confirm        *confirmState
	modelPicker    *modelPickerState
	updateInfo     *update.Info
	checkingUpdate bool
	applyingUpdate bool
	status         string
	statusKind     statusLevel
	selectedKey    string
	pingStatus     map[string]string
}

var (
	titleStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	groupStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("69"))
	currentStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	currentRowStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true)
	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("63")).
			Bold(true)
	addRowStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	helpKeyStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	mutedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	errorStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	successStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	panelStyle      = lipgloss.NewStyle().Border(lipgloss.ASCIIBorder()).BorderForeground(lipgloss.Color("240")).Padding(1, 2)
	labelStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("69")).Bold(true)
	badgeStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).Padding(0, 1).Bold(true)
	headerMetaStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	statusBarStyle  = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)
	formHintStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	panelTitleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	dangerStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	selectedAddStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("230")).
				Background(lipgloss.Color("63")).
				Bold(true)
)

func NewModel(store *ccswitch.Store, warnings []string) (*Model, error) {
	snapshot, err := store.Snapshot()
	if err != nil {
		return nil, err
	}

	model := &Model{
		store:      store,
		width:      100,
		height:     32,
		mode:       modeList,
		current:    snapshot.Current,
		providers:  snapshot.Providers,
		pingStatus: make(map[string]string),
	}
	model.rebuildRows()
	if len(warnings) > 0 {
		model.setStatus(strings.Join(warnings, "；"), statusInfo)
	} else {
		model.setStatus("就绪", statusInfo)
	}

	return model, nil
}

func (m *Model) Init() tea.Cmd {
	return m.checkUpdateCmd(false)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		return m, nil
	case modelFetchResultMsg:
		if typed.err != nil {
			m.form.errorMessage = fmt.Sprintf("获取模型失败: %v", typed.err)
		} else if len(typed.models) == 0 {
			m.form.errorMessage = "未获取到模型"
		} else {
			m.openListPicker(
				fmt.Sprintf("选择 %s 模型", typed.app.DisplayName()),
				typed.models,
				m.form.modelFieldIndex,
			)
			m.form.errorMessage = ""
		}
		return m, nil
	case pingResultMsg:
		key := typed.app.String() + ":" + typed.providerID
		if typed.err != nil {
			m.pingStatus[key] = fmt.Sprintf("✕ %v", typed.err)
		} else {
			m.pingStatus[key] = fmt.Sprintf("✓ %dms %d", typed.latency.Milliseconds(), typed.statusCode)
		}
		return m, nil
	case updateCheckResultMsg:
		return m.handleUpdateCheckResult(typed)
	case updateApplyResultMsg:
		return m.handleUpdateApplyResult(typed)
	}

	if m.applyingUpdate {
		// 更新进行中时忽略普通按键，避免并发操作把状态搞乱
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "ctrl+c":
				return m, tea.Quit
			}
		}
		return m, nil
	}

	// 全局兜底：任何模式下 Ctrl+C 都强制退出（此前只有列表页生效）
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.mode {
	case modeList:
		return m.updateList(msg)
	case modeForm:
		return m.updateForm(msg)
	case modeConfirm:
		return m.updateConfirm(msg)
	case modeModelPicker:
		return m.updateModelPicker(msg)
	case modeUpdateConfirm:
		return m.updateUpdateConfirm(msg)
	default:
		return m, nil
	}
}

func (m *Model) View() string {
	switch m.mode {
	case modeForm:
		return m.viewForm()
	case modeConfirm:
		return m.viewConfirm()
	case modeModelPicker:
		return m.viewModelPicker()
	case modeUpdateConfirm:
		return m.viewUpdateConfirm()
	default:
		return m.viewList()
	}
}

func (m *Model) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		switch typed.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "u":
			if m.checkingUpdate || m.applyingUpdate {
				m.setStatus("正在处理更新，请稍候...", statusInfo)
				return m, nil
			}
			m.setStatus("正在检查更新...", statusInfo)
			return m, m.checkUpdateCmd(true)
		case "up", "k":
			m.moveCursor(-1)
		case "down", "j":
			m.moveCursor(1)
		case "t":
			row := m.selectedRow()
			if row != nil && row.kind == rowProvider && row.provider != nil {
				return m, m.pingProvider(row.app, *row.provider)
			}
		case "g":
			m.moveToEdge(true)
		case "G":
			m.moveToEdge(false)
		case "0":
			m.jumpToApp(ccswitch.AppGlobal)
		case "1":
			m.jumpToApp(ccswitch.AppClaude)
		case "2":
			m.jumpToApp(ccswitch.AppCodex)
		case "3":
			m.jumpToApp(ccswitch.AppPi)
		case "a":
			row := m.selectedRow()
			if row != nil {
				m.openAddForm(row.app)
				return m, textinput.Blink
			}
		case "e":
			row := m.selectedRow()
			if row != nil && row.kind == rowProvider && row.provider != nil {
				m.openEditForm(row.app, *row.provider)
				return m, textinput.Blink
			}
		case "d":
			row := m.selectedRow()
			if row != nil && row.kind == rowProvider && row.provider != nil {
				m.confirm = &confirmState{app: row.app, provider: *row.provider}
				m.mode = modeConfirm
			}
		case "enter":
			row := m.selectedRow()
			if row == nil {
				return m, nil
			}
			switch row.kind {
			case rowAdd:
				m.openAddForm(row.app)
				return m, textinput.Blink
			case rowProvider:
				if row.provider == nil {
					return m, nil
				}
				if row.app == ccswitch.AppGlobal {
					m.setStatus("全局供应商已同步到各 CLI，请到 Claude/Codex/Pi 中按 Enter 切换", statusInfo)
					return m, nil
				}
				if m.current[row.app] == row.provider.ID {
					m.setStatus(fmt.Sprintf("%s 已经是当前供应商", row.provider.Name), statusInfo)
					return m, nil
				}
				if err := m.store.SwitchProvider(row.app, row.provider.ID); err != nil {
					m.setStatus(err.Error(), statusError)
					return m, nil
				}
				m.selectedKey = row.key
				if err := m.reload(); err != nil {
					m.setStatus(err.Error(), statusError)
					return m, nil
				}
				m.setStatus(fmt.Sprintf("已切换 %s -> %s", row.app.DisplayName(), row.provider.Name), statusSuccess)
			}
		}
	}

	return m, nil
}

func (m *Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		switch typed.String() {
		case "esc":
			m.mode = modeList
			m.form = formState{}
			m.setStatus("已取消编辑", statusInfo)
			return m, nil
		case "tab", "down":
			m.form.focusIndex = (m.form.focusIndex + 1) % len(m.form.fields)
			m.syncFormFocus()
			return m, nil
		case "shift+tab", "up":
			m.form.focusIndex--
			if m.form.focusIndex < 0 {
				m.form.focusIndex = len(m.form.fields) - 1
			}
			m.syncFormFocus()
			return m, nil
		case "left", "h":
			if m.cycleSelectField(-1) {
				return m, nil
			}
		case "right", "l":
			if m.cycleSelectField(1) {
				return m, nil
			}
		case "enter", " ":
			if m.openSelectFieldPicker() {
				return m, nil
			}
			if typed.String() == "enter" {
				if m.form.focusIndex >= len(m.form.fields)-1 {
					return m.saveForm()
				}
				m.form.focusIndex = (m.form.focusIndex + 1) % len(m.form.fields)
				m.syncFormFocus()
				return m, nil
			}
		case "ctrl+s":
			return m.saveForm()
		case "ctrl+f":
			if m.form.focusIndex == m.form.modelFieldIndex {
				m.form.errorMessage = "正在获取模型列表..."
				return m, m.fetchModels()
			}
			if m.openSelectFieldPicker() {
				return m, nil
			}
		}
	}

	// 选择型字段不吃自由输入，避免误打一堆垃圾
	if m.isSelectField(m.form.focusIndex) {
		return m, nil
	}

	var cmds []tea.Cmd
	for index := range m.form.fields {
		field, cmd := m.form.fields[index].Update(msg)
		m.form.fields[index] = field
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		switch typed.String() {
		case "esc", "n":
			m.mode = modeList
			m.confirm = nil
			m.setStatus("已取消删除", statusInfo)
			return m, nil
		case "enter", "y":
			if m.confirm == nil {
				m.mode = modeList
				return m, nil
			}
			if m.current[m.confirm.app] == m.confirm.provider.ID && !m.canDeleteCurrentConfirm() {
				return m, nil
			}
			if err := m.store.DeleteProvider(m.confirm.app, m.confirm.provider.ID); err != nil {
				m.mode = modeList
				m.confirm = nil
				m.setStatus(err.Error(), statusError)
				return m, nil
			}
			deletedName := m.confirm.provider.Name
			m.selectedKey = ""
			m.mode = modeList
			m.confirm = nil
			if err := m.reload(); err != nil {
				m.setStatus(err.Error(), statusError)
				return m, nil
			}
			m.setStatus(fmt.Sprintf("已删除 %s", deletedName), statusSuccess)
		}
	}

	return m, nil
}

func (m *Model) updateModelPicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	typed, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	picker := m.modelPicker
	if picker == nil {
		return m, nil
	}

	if picker.filtering {
		switch typed.String() {
		case "esc":
			picker.filtering = false
		case "enter":
			return m.confirmModelPickerSelection()
		case "backspace":
			runes := []rune(picker.filter)
			if len(runes) > 0 {
				picker.filter = string(runes[:len(runes)-1])
				picker.cursor = 0
			}
		case "space":
			picker.filter += " "
			picker.cursor = 0
		case "up", "k":
			if picker.cursor > 0 {
				picker.cursor--
			}
		case "down", "j":
			if picker.cursor < len(picker.visible())-1 {
				picker.cursor++
			}
		default:
			if typed.Type == tea.KeyRunes {
				picker.filter += string(typed.Runes)
				picker.cursor = 0
			}
		}
		return m, nil
	}

	switch typed.String() {
	case "esc":
		m.mode = modeForm
		m.modelPicker = nil
		return m, nil
	case "/":
		picker.filtering = true
	case "up", "k":
		if picker.cursor > 0 {
			picker.cursor--
		}
	case "down", "j":
		if picker.cursor < len(picker.visible())-1 {
			picker.cursor++
		}
	case "enter":
		return m.confirmModelPickerSelection()
	}
	return m, nil
}

func (m *Model) confirmModelPickerSelection() (tea.Model, tea.Cmd) {
	picker := m.modelPicker
	visible := picker.visible()
	if len(visible) == 0 {
		return m, nil
	}
	if picker.cursor >= len(visible) {
		picker.cursor = len(visible) - 1
	}
	selected := visible[picker.cursor]
	target := picker.targetField
	if target < 0 || target >= len(m.form.fields) {
		target = m.form.modelFieldIndex
	}
	// 显示层把空值映射成「默认」/带说明文案，写回时还原
	if selected == "默认" || selected == "(默认)" {
		selected = ""
	}
	if selected == "responses (推荐)" {
		selected = "responses"
	}
	m.form.fields[target].SetValue(selected)
	m.mode = modeForm
	m.modelPicker = nil
	if target+1 < len(m.form.fields) {
		m.form.focusIndex = target + 1
	}
	m.syncFormFocus()
	return m, nil
}

func (m *Model) fetchModels() tea.Cmd {
	app := m.form.app
	baseURL := strings.TrimSpace(m.form.fields[1].Value())
	apiKey := strings.TrimSpace(m.form.fields[2].Value())

	if baseURL == "" {
		switch app {
		case ccswitch.AppClaude:
			baseURL = "https://api.anthropic.com"
		case ccswitch.AppCodex, ccswitch.AppPi, ccswitch.AppGlobal:
			baseURL = "https://api.openai.com"
		}
	}

	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "https://" + baseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	modelsBaseURL := strings.TrimSuffix(baseURL, "/v1")

	return func() tea.Msg {
		buildRequest := func(url string) (*http.Request, error) {
			req, err := http.NewRequest("GET", url, nil)
			if err != nil || apiKey == "" {
				return req, err
			}
			switch app {
			case ccswitch.AppClaude:
				req.Header.Set("x-api-key", apiKey)
				req.Header.Set("anthropic-version", "2023-06-01")
			default:
				req.Header.Set("Authorization", "Bearer "+apiKey)
			}
			return req, nil
		}

		urls := []string{
			modelsBaseURL + "/v1/models",
			modelsBaseURL + "/models",
		}

		client := &http.Client{Timeout: 10 * time.Second}
		var body []byte
		var lastErr error
		for _, url := range urls {
			req, err := buildRequest(url)
			if err != nil {
				return modelFetchResultMsg{app: app, err: err}
			}

			resp, err := client.Do(req)
			if err != nil {
				lastErr = err
				continue
			}

			body, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				lastErr = err
				continue
			}
			if resp.StatusCode == 200 {
				lastErr = nil
				break
			}
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 100))
		}

		if lastErr != nil {
			return modelFetchResultMsg{app: app, err: lastErr}
		}

		var models []string
		var result struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return modelFetchResultMsg{app: app, err: err}
		}
		for _, item := range result.Data {
			models = append(models, item.ID)
		}

		if len(models) == 0 {
			return modelFetchResultMsg{app: app, err: fmt.Errorf("API 返回了空的模型列表")}
		}

		return modelFetchResultMsg{app: app, models: models}
	}
}

func (m *Model) openAddForm(app ccswitch.AppType) {
	m.mode = modeForm
	m.form = newFormState(app, nil, ccswitch.ProviderInput{})
}

func (m *Model) openEditForm(app ccswitch.AppType, provider ccswitch.Provider) {
	m.mode = modeForm
	m.form = newFormState(app, &provider, m.store.ExtractInput(app, provider))
}

func (m *Model) saveForm() (tea.Model, tea.Cmd) {
	input := m.formInput()
	if strings.TrimSpace(input.Name) == "" {
		m.form.errorMessage = "Name is required"
		return m, nil
	}

	var statusMessage string

	if m.form.editMode && m.form.original != nil {
		updated, err := m.store.UpdateProvider(m.form.app, *m.form.original, input)
		if err != nil {
			m.form.errorMessage = err.Error()
			return m, nil
		}
		m.selectedKey = providerKey(m.form.app, updated.ID)
		if m.form.app == ccswitch.AppGlobal {
			statusMessage = fmt.Sprintf("已更新全局供应商 %s，并同步到 Claude/Codex/Pi", updated.Name)
		} else {
			statusMessage = fmt.Sprintf("已更新 %s", updated.Name)
		}
	} else {
		created, autoSwitched, err := m.store.AddProvider(m.form.app, input)
		if err != nil {
			m.form.errorMessage = err.Error()
			return m, nil
		}
		m.selectedKey = providerKey(m.form.app, created.ID)
		if m.form.app == ccswitch.AppGlobal {
			statusMessage = fmt.Sprintf("已添加全局供应商 %s，并同步到 Claude/Codex/Pi（未自动切换）", created.Name)
		} else {
			statusMessage = fmt.Sprintf("已添加 %s", created.Name)
			if autoSwitched {
				statusMessage += "，并自动设为当前供应商"
			}
		}
	}

	m.mode = modeList
	m.form = formState{}
	if err := m.reload(); err != nil {
		m.setStatus(err.Error(), statusError)
		return m, nil
	}
	m.setStatus(statusMessage, statusSuccess)
	return m, nil
}

func (m *Model) reload() error {
	snapshot, err := m.store.Snapshot()
	if err != nil {
		return err
	}
	m.providers = snapshot.Providers
	m.current = snapshot.Current
	m.rebuildRows()
	return nil
}

func (m *Model) rebuildRows() {
	rows := make([]listRow, 0, 32)
	for index, app := range ccswitch.UIAppTypes {
		if index > 0 {
			rows = append(rows, listRow{kind: rowSpacer, key: fmt.Sprintf("spacer:%d", index)})
		}
		rows = append(rows, listRow{
			kind: rowHeading,
			app:  app,
			key:  headingKey(app),
		})
		for index := range m.providers[app] {
			provider := m.providers[app][index]
			copyProvider := provider
			rows = append(rows, listRow{
				kind:     rowProvider,
				app:      app,
				provider: &copyProvider,
				key:      providerKey(app, provider.ID),
			})
		}
		rows = append(rows, listRow{
			kind: rowAdd,
			app:  app,
			key:  addKey(app),
		})
	}

	m.rows = rows
	if len(m.rows) == 0 {
		m.cursor = 0
		return
	}

	if m.selectedKey != "" {
		for index, row := range m.rows {
			if row.key == m.selectedKey {
				m.cursor = index
				return
			}
		}
	}

	for index, row := range m.rows {
		if isSelectableRow(row) {
			m.cursor = index
			return
		}
	}
	m.cursor = 0
}

func (m *Model) moveCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}
	next := m.cursor
	for {
		next += delta
		if next < 0 || next >= len(m.rows) {
			return
		}
		if isSelectableRow(m.rows[next]) {
			m.cursor = next
			m.selectedKey = m.rows[next].key
			return
		}
	}
}

func (m *Model) moveToEdge(top bool) {
	if len(m.rows) == 0 {
		return
	}

	if top {
		for index, row := range m.rows {
			if isSelectableRow(row) {
				m.cursor = index
				m.selectedKey = row.key
				return
			}
		}
		return
	}

	for index := len(m.rows) - 1; index >= 0; index-- {
		if isSelectableRow(m.rows[index]) {
			m.cursor = index
			m.selectedKey = m.rows[index].key
			return
		}
	}
}

func (m *Model) jumpToApp(app ccswitch.AppType) {
	for index, row := range m.rows {
		if row.app == app && isSelectableRow(row) {
			m.cursor = index
			m.selectedKey = row.key
			return
		}
	}
}

func (m *Model) selectedRow() *listRow {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	if !isSelectableRow(m.rows[m.cursor]) {
		return nil
	}
	return &m.rows[m.cursor]
}

func (m *Model) pingProvider(app ccswitch.AppType, provider ccswitch.Provider) tea.Cmd {
	input := m.store.ExtractInput(app, provider)
	baseURL := strings.TrimSpace(input.BaseURL)
	if baseURL == "" {
		switch app {
		case ccswitch.AppClaude:
			baseURL = "https://api.anthropic.com"
		case ccswitch.AppCodex, ccswitch.AppPi:
			baseURL = "https://api.openai.com"
		}
	}
	providerID := provider.ID
	return func() tea.Msg {
		start := time.Now()
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(baseURL)
		elapsed := time.Since(start)
		if err != nil {
			return pingResultMsg{providerID: providerID, app: app, err: err}
		}
		resp.Body.Close()
		return pingResultMsg{providerID: providerID, app: app, latency: elapsed, statusCode: resp.StatusCode}
	}
}

func (m *Model) checkUpdateCmd(manual bool) tea.Cmd {
	if m.checkingUpdate || m.applyingUpdate {
		return nil
	}
	// 开发版默认不自动联网检查，避免本地 go run 每次被正式版弹窗打扰
	if !manual && (Version == "" || Version == "dev") {
		return nil
	}
	m.checkingUpdate = true
	current := Version
	return func() tea.Msg {
		info, err := update.Check(current)
		return updateCheckResultMsg{info: info, err: err, manual: manual}
	}
}

func (m *Model) applyUpdateCmd(info *update.Info) tea.Cmd {
	if info == nil {
		return nil
	}
	m.applyingUpdate = true
	return func() tea.Msg {
		path, err := update.Apply(info)
		return updateApplyResultMsg{path: path, version: info.Latest, err: err}
	}
}

func (m *Model) handleUpdateCheckResult(msg updateCheckResultMsg) (tea.Model, tea.Cmd) {
	m.checkingUpdate = false

	if msg.err != nil {
		if msg.manual {
			m.setStatus(fmt.Sprintf("检查更新失败: %v", msg.err), statusError)
		}
		// 自动检查失败保持安静，避免启动时刷网络错误
		return m, nil
	}

	if msg.info == nil {
		if msg.manual {
			m.setStatus(fmt.Sprintf("已是最新版本 (%s)", update.Normalize(Version)), statusSuccess)
		}
		return m, nil
	}

	// 列表/空闲时才打断用户；手动检查可从任意列表态进入
	if m.mode != modeList && !msg.manual {
		m.updateInfo = msg.info
		m.setStatus(fmt.Sprintf("发现新版本 v%s，按 u 查看", msg.info.Latest), statusInfo)
		return m, nil
	}

	m.updateInfo = msg.info
	m.mode = modeUpdateConfirm
	m.setStatus(fmt.Sprintf("发现新版本 v%s", msg.info.Latest), statusInfo)
	return m, nil
}

func (m *Model) handleUpdateApplyResult(msg updateApplyResultMsg) (tea.Model, tea.Cmd) {
	m.applyingUpdate = false
	m.mode = modeList
	m.updateInfo = nil

	if msg.err != nil {
		m.setStatus(fmt.Sprintf("更新失败: %v", msg.err), statusError)
		return m, nil
	}

	m.setStatus(fmt.Sprintf("已更新到 v%s，请重启 cctui 生效（%s）", msg.version, msg.path), statusSuccess)
	return m, nil
}

func (m *Model) updateUpdateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		switch typed.String() {
		case "esc", "n":
			m.mode = modeList
			m.updateInfo = nil
			m.setStatus("已跳过本次更新", statusInfo)
			return m, nil
		case "enter", "y":
			if m.updateInfo == nil {
				m.mode = modeList
				return m, nil
			}
			m.setStatus(fmt.Sprintf("正在下载并安装 v%s ...", m.updateInfo.Latest), statusInfo)
			return m, m.applyUpdateCmd(m.updateInfo)
		}
	}
	return m, nil
}

func (m *Model) viewUpdateConfirm() string {
	if m.updateInfo == nil {
		return m.viewList()
	}

	title := "发现新版本"
	if m.applyingUpdate {
		title = "正在更新"
	}

	notes := strings.TrimSpace(m.updateInfo.Notes)
	if notes == "" {
		notes = "无更新说明"
	}
	noteLines := wrapText(notes, max(24, min(m.width-12, 72)))
	if len(noteLines) > 6 {
		noteLines = append(noteLines[:6], "...")
	}

	body := []string{
		panelTitleStyle.Render(title),
		"",
		fmt.Sprintf("当前版本: %s", displayVersion(Version)),
		fmt.Sprintf("最新版本: v%s", m.updateInfo.Latest),
		fmt.Sprintf("发布源:   %s", m.updateInfo.Source),
		fmt.Sprintf("安装位置: %s", firstNonEmptyLocal(m.updateInfo.Executable, "(未知)")),
		"",
		labelStyle.Render("更新说明"),
	}
	body = append(body, noteLines...)
	body = append(body, "")
	if m.applyingUpdate {
		body = append(body, successStyle.Render("正在下载并替换二进制，请稍候..."))
	} else {
		body = append(body,
			"确认后将自动下载预编译包并替换当前程序。",
			"旧版本会备份为 cctui.bak。",
			"按 Enter / y 立即更新，Esc / n 稍后处理。",
		)
	}

	panelLines := strings.Split(panelStyle.Width(max(50, min(m.width-8, 80))).Render(strings.Join(body, "\n")), "\n")
	helpLines := m.renderHelpLines()
	page := []string{m.renderHeader()}
	topPadding := (m.height - len(panelLines) - len(helpLines) - 1) / 2
	if topPadding < 1 {
		topPadding = 1
	}
	for i := 0; i < topPadding; i++ {
		page = append(page, "")
	}
	page = append(page, panelLines...)
	for len(page)+len(helpLines) < m.height {
		page = append(page, "")
	}
	page = append(page, helpLines...)
	return strings.Join(page, "\n")
}

func displayVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "dev"
	}
	if version == "dev" || strings.HasPrefix(version, "v") || strings.HasPrefix(version, "V") {
		return version
	}
	return "v" + version
}

func firstNonEmptyLocal(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (m *Model) setStatus(message string, kind statusLevel) {
	m.status = message
	m.statusKind = kind
}

func (m *Model) viewList() string {
	helpLines := m.renderHelpLines()
	statusLines := strings.Split(m.renderStatusLine(), "\n")
	bodyHeight := m.height - 2 - len(statusLines) - len(helpLines)
	if bodyHeight < 6 {
		bodyHeight = 6
	}

	start := 0
	if len(m.rows) > bodyHeight {
		start = m.cursor - bodyHeight/2
		if start < 0 {
			start = 0
		}
		if start > len(m.rows)-bodyHeight {
			start = len(m.rows) - bodyHeight
		}
	}

	end := start + bodyHeight
	if end > len(m.rows) {
		end = len(m.rows)
	}

	lines := []string{m.renderHeader(), ""}
	for index := start; index < end; index++ {
		row := m.rows[index]
		switch row.kind {
		case rowHeading:
			lines = append(lines, m.renderGroupHeading(row.app))
		case rowSpacer:
			lines = append(lines, "")
		case rowProvider:
			lines = append(lines, m.renderProviderRow(index, row))
		case rowAdd:
			lines = append(lines, m.renderAddRow(index, row))
		}
	}
	for len(lines) < 2+bodyHeight {
		lines = append(lines, "")
	}
	lines = append(lines, statusLines...)
	lines = append(lines, helpLines...)
	return strings.Join(lines, "\n")
}

func (m *Model) viewModelPicker() string {
	picker := m.modelPicker
	if picker == nil {
		return ""
	}

	title := picker.title
	if title == "" {
		title = fmt.Sprintf("选择 %s 模型", picker.app.DisplayName())
	}
	lines := []string{panelTitleStyle.Render(title)}

	visible := picker.visible()
	if picker.filtering || picker.filter != "" {
		filterLine := "/ " + picker.filter
		if picker.filtering {
			filterLine += "▏"
		}
		lines = append(lines, formHintStyle.Render(filterLine)+mutedStyle.Render(fmt.Sprintf("  %d/%d", len(visible), len(picker.models))))
	}
	lines = append(lines, "")

	bodyHeight := max(6, m.height-9)
	start := 0
	if picker.cursor >= bodyHeight {
		start = picker.cursor - bodyHeight + 1
	}

	for i := start; i < len(visible) && i < start+bodyHeight; i++ {
		model := visible[i]
		prefix := "  "
		if i == picker.cursor {
			prefix = "▶ "
			lines = append(lines, selectedStyle.Render(prefix+model))
		} else {
			lines = append(lines, prefix+model)
		}
	}
	if len(visible) == 0 {
		lines = append(lines, mutedStyle.Render("  无匹配结果"))
	}

	lines = append(lines, "")
	if picker.filtering {
		lines = append(lines, formHintStyle.Render("输入过滤  ↑/↓ 移动  Enter 选择  Esc 结束过滤"))
	} else {
		lines = append(lines, formHintStyle.Render("↑/↓ 移动  Enter 选择  / 过滤  Esc 取消"))
	}

	panelLines := strings.Split(panelStyle.Width(max(50, min(m.width-4, 80))).Render(strings.Join(lines, "\n")), "\n")
	page := []string{m.renderHeader()}
	topPad := max(1, (m.height-len(panelLines)-2)/2)
	for i := 0; i < topPad; i++ {
		page = append(page, "")
	}
	page = append(page, panelLines...)
	return strings.Join(page, "\n")
}

func (m *Model) viewForm() string {
	title := "新增 " + m.form.app.DisplayName() + " 供应商"
	if m.form.app == ccswitch.AppGlobal {
		title = "新增全局供应商"
	}
	if m.form.editMode {
		title = "编辑 " + m.form.app.DisplayName() + " 供应商"
		if m.form.app == ccswitch.AppGlobal {
			title = "编辑全局供应商"
		}
	}

	lines := []string{
		panelTitleStyle.Render(title),
		formHintStyle.Render(m.formHint()),
		"",
	}
	for index, field := range m.form.fields {
		label := m.form.labels[index]
		lines = append(lines, labelStyle.Render(label))
		if m.isSelectField(index) {
			lines = append(lines, m.renderSelectField(index, field))
		} else {
			lines = append(lines, field.View())
		}
		lines = append(lines, "")
	}
	if m.form.errorMessage != "" {
		lines = append(lines, errorStyle.Render(m.form.errorMessage))
	}
	panelLines := strings.Split(panelStyle.Width(max(60, min(m.width-4, 100))).Render(strings.Join(lines, "\n")), "\n")
	helpLines := m.renderHelpLines()
	page := []string{m.renderHeader(), ""}
	page = append(page, panelLines...)
	for len(page)+len(helpLines) < m.height {
		page = append(page, "")
	}
	page = append(page, helpLines...)
	return strings.Join(page, "\n")
}

func (m *Model) viewConfirm() string {
	if m.confirm == nil {
		return ""
	}

	title := "Delete Provider"
	canDeleteCurrent := m.canDeleteCurrentConfirm()
	body := []string{
		dangerStyle.Render(title),
		"",
		fmt.Sprintf("App: %s", m.confirm.app.DisplayName()),
		fmt.Sprintf("Provider: %s", m.confirm.provider.Name),
		"",
		"The current provider cannot be deleted.",
		"Switch to another provider first.",
	}

	if m.confirm.app == ccswitch.AppGlobal {
		body = []string{
			dangerStyle.Render("删除全局供应商"),
			"",
			fmt.Sprintf("名称: %s", m.confirm.provider.Name),
		}
		body = append(body, providerURLLines(m.store, m.confirm.app, m.confirm.provider, max(24, min(m.width-8, 80)-6))...)
		body = append(body,
			"",
			"将删除该全局模板，并尝试删除 Claude/Codex/Pi 中的关联副本。",
			"若某 CLI 正在使用该副本且还有其他供应商，则该副本会保留。",
			"按 Enter / y 确认，Esc / n 返回。",
		)
	} else if m.current[m.confirm.app] != m.confirm.provider.ID {
		body = []string{
			dangerStyle.Render(title),
			"",
			fmt.Sprintf("App: %s", m.confirm.app.DisplayName()),
			fmt.Sprintf("Provider: %s", m.confirm.provider.Name),
		}
		body = append(body, providerURLLines(m.store, m.confirm.app, m.confirm.provider, max(24, min(m.width-8, 80)-6))...)
		body = append(body,
			"",
			"This will remove the provider record from the database.",
			"Press Enter / y to confirm, Esc / n to go back.",
		)
	} else if canDeleteCurrent {
		body = []string{
			dangerStyle.Render(title),
			"",
			fmt.Sprintf("App: %s", m.confirm.app.DisplayName()),
			fmt.Sprintf("Provider: %s", m.confirm.provider.Name),
			"",
			"This is the last provider for the app.",
			"After deletion, the app will have no active provider.",
			"Press Enter / y to confirm, Esc / n to go back.",
		}
	}

	panelLines := strings.Split(panelStyle.Width(max(50, min(m.width-8, 80))).Render(strings.Join(body, "\n")), "\n")
	helpLines := m.renderHelpLines()
	page := []string{m.renderHeader()}
	topPadding := (m.height - len(panelLines) - len(helpLines) - 1) / 2
	if topPadding < 1 {
		topPadding = 1
	}
	for i := 0; i < topPadding; i++ {
		page = append(page, "")
	}
	page = append(page, panelLines...)
	for len(page)+len(helpLines) < m.height {
		page = append(page, "")
	}
	page = append(page, helpLines...)
	return strings.Join(page, "\n")
}

func (m *Model) renderHeader() string {
	left := titleStyle.Render("cctui "+Version) + " " + badgeStyle.Render(m.modeLabel())
	totalWidth := max(40, m.width)
	rightText := m.renderHeaderMeta(max(0, totalWidth-lipgloss.Width(left)-1))
	if rightText == "" {
		return left
	}

	right := headerMetaStyle.Render(rightText)
	gap := totalWidth - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) renderHeaderMeta(maxWidth int) string {
	if maxWidth < 8 {
		return ""
	}

	full := make([]string, 0, len(ccswitch.AllAppTypes))
	compact := make([]string, 0, len(ccswitch.AllAppTypes))

	for _, app := range ccswitch.AllAppTypes {
		currentName := "-"
		if currentID, ok := m.current[app]; ok && currentID != "" {
			for _, p := range m.providers[app] {
				if p.ID == currentID {
					currentName = p.Name
					break
				}
			}
		}
		full = append(full, fmt.Sprintf("%s: %s", app.DisplayName(), currentName))
		compact = append(compact, fmt.Sprintf("%c: %s", []rune(app.DisplayName())[0], currentName))
	}

	candidates := []string{
		strings.Join(full, " | "),
		strings.Join(compact, " | "),
	}

	for _, candidate := range candidates {
		if lipgloss.Width(candidate) <= maxWidth {
			return candidate
		}
	}

	return truncate(strings.Join(compact, " | "), maxWidth)
}

func (m *Model) renderGroupHeading(app ccswitch.AppType) string {
	label := groupStyle.Render(app.DisplayName())
	currentName := ""
	if app.IsLiveApp() {
		if currentID, ok := m.current[app]; ok && currentID != "" {
			for _, p := range m.providers[app] {
				if p.ID == currentID {
					currentName = " → " + currentStyle.Render(p.Name)
					break
				}
			}
		}
	}
	count := len(m.providers[app])
	suffix := "个供应商"
	if app == ccswitch.AppGlobal {
		suffix = "个全局模板"
	}
	summary := mutedStyle.Render(fmt.Sprintf("(%d %s", count, suffix)) + currentName + mutedStyle.Render(")")
	return label + " " + summary
}

func (m *Model) renderProviderRow(index int, row listRow) string {
	if row.provider == nil {
		return ""
	}

	selected := index == m.cursor
	isCurrent := row.app.IsLiveApp() && m.current[row.app] == row.provider.ID
	fromGlobal := stringValueAny(row.provider.Meta["from_global"]) == "true" || stringValueAny(row.provider.Meta["global_id"]) != ""
	prefix := "  "
	if selected {
		prefix = "▶ "
	}
	currentMark := " "
	if isCurrent {
		currentMark = "●"
	} else if row.app == ccswitch.AppGlobal {
		currentMark = "◎"
	} else if fromGlobal {
		currentMark = "↻"
	}

	nameWidth, endpointWidth := m.providerColumnWidths(isCurrent)
	name := padRight(truncate(row.provider.Name, nameWidth), nameWidth)
	endpoint := padRight(truncate(m.store.EndpointSummary(row.app, *row.provider), endpointWidth), endpointWidth)

	ping := ""
	pingKey := row.app.String() + ":" + row.provider.ID
	if ps, ok := m.pingStatus[pingKey]; ok {
		ping = " " + mutedStyle.Render(ps)
	}

	line := strings.TrimRight(fmt.Sprintf("%s%s %s %s%s", prefix, currentMark, name, endpoint, ping), " ")
	if selected {
		return selectedStyle.Render(line)
	}
	if isCurrent {
		return currentRowStyle.Render(line)
	}
	return line
}

func (m *Model) renderAddRow(index int, row listRow) string {
	selected := index == m.cursor
	prefix := "  "
	if selected {
		prefix = "▶ "
	}
	label := row.app.DisplayName()
	if row.app == ccswitch.AppGlobal {
		label = "全局"
	}
	line := truncate(fmt.Sprintf("%s+ 添加 %s 供应商", prefix, label), max(20, m.width-2))
	if selected {
		return selectedAddStyle.Render(line)
	}
	return addRowStyle.Render(line)
}

func (m *Model) renderStatusLine() string {
	text := m.status
	if text == "" {
		text = "就绪"
	}
	prefix := "信息"
	switch m.statusKind {
	case statusError:
		prefix = "错误"
	case statusSuccess:
		prefix = "完成"
	}
	segments := []string{prefix + " · " + strings.ReplaceAll(text, "\n", " ")}
	segments = append(segments, m.selectedProviderStatusSegments()...)
	content := strings.Join(segments, " | ")
	contentWidth := max(16, m.width-4)
	return statusBarStyle.Width(max(24, m.width)).Render(strings.Join(wrapText(content, contentWidth), "\n"))
}

func (m *Model) selectedProviderStatusSegments() []string {
	row := m.selectedRow()
	if row == nil || row.kind != rowProvider || row.provider == nil {
		return nil
	}

	input := m.store.ExtractInput(row.app, *row.provider)
	baseURL := strings.TrimSpace(input.BaseURL)
	if baseURL == "" {
		baseURL = providerBaseURLFallback(row.app)
	}

	segments := []string{"Base URL: " + baseURL}
	if website := strings.TrimSpace(input.Website); website != "" {
		segments = append(segments, "Website: "+website)
	}
	return segments
}

func (m *Model) renderHelpLines() []string {
	var items []string
	switch m.mode {
	case modeForm:
		items = []string{
			help("Enter", "选择/下一项/保存"),
			help("←/→", "切换选项"),
			help("Tab", "下一项"),
			help("Shift+Tab", "上一项"),
			help("Ctrl+F", "获取模型/打开选项"),
			help("Ctrl+S", "保存"),
			help("Esc", "返回"),
		}
	case modeConfirm:
		if m.confirm != nil && m.current[m.confirm.app] == m.confirm.provider.ID && !m.canDeleteCurrentConfirm() {
			items = []string{help("Esc", "返回")}
		} else {
			items = []string{
				help("Enter/y", "确认"),
				help("Esc/n", "返回"),
			}
		}
	case modeUpdateConfirm:
		if m.applyingUpdate {
			items = []string{help("ctrl+c", "强制退出")}
		} else {
			items = []string{
				help("Enter/y", "立即更新"),
				help("Esc/n", "稍后"),
			}
		}
	default:
		items = []string{
			help("↑/↓ j/k", "移动"),
			help("Enter", "设为当前"),
			help("t", "测速"),
			help("u", "检查更新"),
			help("a", "添加"),
			help("e", "编辑"),
			help("d", "删除"),
			help("0-3", "跳分组"),
			help("g/G", "顶/底"),
			help("Esc", "退出"),
		}
	}
	return wrapInlineItems(items, max(20, m.width-2))
}

func newFormState(app ccswitch.AppType, provider *ccswitch.Provider, input ccswitch.ProviderInput) formState {
	labels, values := buildFormFields(app, input)

	fields := make([]textinput.Model, 0, len(labels))
	modelFieldIndex := 3
	for index, label := range labels {
		field := textinput.New()
		field.SetValue(values[index])
		field.Prompt = "› "
		field.CharLimit = 2048
		field.Width = 72
		field.Placeholder = placeholderFor(app, label)
		fields = append(fields, field)
		if label == "Model" {
			modelFieldIndex = index
		}
	}

	state := formState{
		app:             app,
		editMode:        provider != nil,
		original:        provider,
		modelFieldIndex: modelFieldIndex,
		fields:          fields,
		labels:          labels,
		focusIndex:      0,
	}
	state.syncFocus()
	return state
}

func buildFormFields(app ccswitch.AppType, input ccswitch.ProviderInput) ([]string, []string) {
	labels := []string{"Name", "Base URL", "API Key", "Model"}
	values := []string{input.Name, input.BaseURL, input.APIKey, input.Model}

	add := func(label, value string) {
		labels = append(labels, label)
		values = append(values, value)
	}
	intOrEmpty := func(v int) string {
		if v <= 0 {
			return ""
		}
		return fmt.Sprintf("%d", v)
	}
	piContext := input.ContextWindow
	if piContext <= 0 && (app == ccswitch.AppPi || app == ccswitch.AppGlobal) {
		// Pi 默认展示 128000；全局也给个可改的默认值方便同步
		if app == ccswitch.AppPi {
			piContext = 128000
		}
	}
	reasoning := strings.TrimSpace(input.Reasoning)
	if reasoning == "" && (app == ccswitch.AppPi || app == ccswitch.AppGlobal) {
		reasoning = "true"
	}
	apiType := strings.TrimSpace(input.APIType)
	if apiType == "" && (app == ccswitch.AppPi || app == ccswitch.AppGlobal) {
		apiType = "openai-completions"
	}
	wireAPI := strings.TrimSpace(input.WireAPI)
	if wireAPI == "" && (app == ccswitch.AppCodex || app == ccswitch.AppGlobal) {
		wireAPI = "responses"
	}

	switch app {
	case ccswitch.AppClaude:
		add("Reasoning Model", input.ReasoningModel)
		add("Haiku/Fast Model", input.HaikuModel)
	case ccswitch.AppCodex:
		add("Reasoning Effort", input.ReasoningEffort)
		add("Reasoning Summary", input.ReasoningSummary)
		add("Verbosity", input.ModelVerbosity)
		add("Service Tier", input.ServiceTier)
		add("Wire API", wireAPI)
		add("Context Window", intOrEmpty(input.ContextWindow))
	case ccswitch.AppPi:
		add("API Type", apiType)
		add("Context Window", fmt.Sprintf("%d", piContext))
		add("Max Tokens", intOrEmpty(input.MaxTokens))
		add("Reasoning", reasoning)
		add("Max Tokens Field", input.MaxTokensField)
		add("Supports Developer Role", input.SupportsDeveloperRole)
		add("Supports Reasoning Effort", input.SupportsReasoningEffort)
		add("Supports Usage In Streaming", input.SupportsUsageInStreaming)
	case ccswitch.AppGlobal:
		add("Reasoning Effort", input.ReasoningEffort)
		add("Reasoning Summary", input.ReasoningSummary)
		add("Verbosity", input.ModelVerbosity)
		add("Service Tier", input.ServiceTier)
		add("Wire API", wireAPI)
		add("Context Window", intOrEmpty(input.ContextWindow))
		add("Max Tokens", intOrEmpty(input.MaxTokens))
		add("API Type", apiType)
		add("Reasoning", reasoning)
		add("Reasoning Model", input.ReasoningModel)
		add("Haiku/Fast Model", input.HaikuModel)
		add("Max Tokens Field", input.MaxTokensField)
		add("Supports Developer Role", input.SupportsDeveloperRole)
		add("Supports Reasoning Effort", input.SupportsReasoningEffort)
		add("Supports Usage In Streaming", input.SupportsUsageInStreaming)
	}

	add("Website", input.Website)
	add("Notes", input.Notes)
	return labels, values
}

func (m *Model) syncFormFocus() {
	m.form.syncFocus()
}

func (f *formState) syncFocus() {
	for index := range f.fields {
		if index == f.focusIndex {
			f.fields[index].Focus()
			f.fields[index].PromptStyle = currentStyle
			f.fields[index].TextStyle = lipgloss.NewStyle().Bold(true)
			continue
		}
		f.fields[index].Blur()
		f.fields[index].PromptStyle = lipgloss.NewStyle()
		f.fields[index].TextStyle = lipgloss.NewStyle()
	}
}

func (m *Model) formInput() ccswitch.ProviderInput {
	val := func(label string) string {
		for index, name := range m.form.labels {
			if name == label && index < len(m.form.fields) {
				return strings.TrimSpace(m.form.fields[index].Value())
			}
		}
		return ""
	}
	intVal := func(label string) int {
		text := val(label)
		if text == "" {
			return 0
		}
		n, err := strconv.Atoi(text)
		if err != nil {
			return 0
		}
		return n
	}

	return ccswitch.ProviderInput{
		Name:                     val("Name"),
		BaseURL:                  val("Base URL"),
		APIKey:                   val("API Key"),
		Model:                    val("Model"),
		ReasoningModel:           val("Reasoning Model"),
		HaikuModel:               val("Haiku/Fast Model"),
		ReasoningEffort:          val("Reasoning Effort"),
		ReasoningSummary:         val("Reasoning Summary"),
		ModelVerbosity:           val("Verbosity"),
		ServiceTier:              val("Service Tier"),
		WireAPI:                  val("Wire API"),
		APIType:                  val("API Type"),
		ContextWindow:            intVal("Context Window"),
		MaxTokens:                intVal("Max Tokens"),
		Reasoning:                val("Reasoning"),
		MaxTokensField:           val("Max Tokens Field"),
		SupportsDeveloperRole:    val("Supports Developer Role"),
		SupportsReasoningEffort:  val("Supports Reasoning Effort"),
		SupportsUsageInStreaming: val("Supports Usage In Streaming"),
		Website:                  val("Website"),
		Notes:                    val("Notes"),
	}
}

func (m *Model) openListPicker(title string, items []string, targetField int) {
	displayItems := make([]string, len(items))
	cursor := 0
	current := ""
	if targetField >= 0 && targetField < len(m.form.fields) {
		current = strings.TrimSpace(m.form.fields[targetField].Value())
	}
	label := ""
	if targetField >= 0 && targetField < len(m.form.labels) {
		label = m.form.labels[targetField]
	}
	for i, item := range items {
		display := item
		if item == "" && selectFieldsWithDefault[label] {
			display = "默认"
		}
		if label == "Wire API" && item == "responses" {
			display = "responses (推荐)"
		}
		displayItems[i] = display
		if item == current || (item == "" && current == "") {
			cursor = i
		}
	}
	m.modelPicker = &modelPickerState{
		app:         m.form.app,
		title:       title,
		models:      displayItems,
		cursor:      cursor,
		targetField: targetField,
	}
	m.mode = modeModelPicker
}

func (m *Model) isSelectField(index int) bool {
	if index < 0 || index >= len(m.form.labels) {
		return false
	}
	switch m.form.labels[index] {
	case "Reasoning Effort", "Reasoning Summary", "Verbosity", "Service Tier",
		"Wire API", "API Type", "Reasoning", "Max Tokens Field",
		"Supports Developer Role", "Supports Reasoning Effort", "Supports Usage In Streaming":
		return true
	default:
		return false
	}
}

func (m *Model) selectOptionsForField(index int) []string {
	if index < 0 || index >= len(m.form.labels) {
		return nil
	}
	switch m.form.labels[index] {
	case "Reasoning Effort":
		return reasoningEffortOptions
	case "Reasoning Summary":
		return reasoningSummaryOptions
	case "Verbosity":
		return modelVerbosityOptions
	case "Service Tier":
		return serviceTierOptions
	case "Wire API":
		return wireAPIOptions
	case "API Type":
		return piAPITypeOptions
	case "Reasoning":
		return piReasoningOptions
	case "Max Tokens Field":
		return maxTokensFieldOptions
	case "Supports Developer Role", "Supports Reasoning Effort", "Supports Usage In Streaming":
		return triStateOptions
	default:
		return nil
	}
}

func (m *Model) openSelectFieldPicker() bool {
	if !m.isSelectField(m.form.focusIndex) {
		return false
	}
	options := m.selectOptionsForField(m.form.focusIndex)
	if len(options) == 0 {
		return false
	}
	title := "选择 " + m.form.labels[m.form.focusIndex]
	m.openListPicker(title, options, m.form.focusIndex)
	return true
}

func (m *Model) cycleSelectField(delta int) bool {
	index := m.form.focusIndex
	if !m.isSelectField(index) {
		return false
	}
	options := m.selectOptionsForField(index)
	if len(options) == 0 {
		return false
	}
	current := strings.TrimSpace(m.form.fields[index].Value())
	pos := 0
	found := false
	for i, option := range options {
		if option == current {
			pos = i
			found = true
			break
		}
	}
	if !found && current != "" {
		// 未知值时，从相邻项开始
		if delta > 0 {
			pos = -1
		} else {
			pos = 0
		}
	}
	pos = (pos + delta) % len(options)
	if pos < 0 {
		pos += len(options)
	}
	m.form.fields[index].SetValue(options[pos])
	return true
}

func (m *Model) renderSelectField(index int, field textinput.Model) string {
	value := strings.TrimSpace(field.Value())
	display := value
	label := ""
	if index >= 0 && index < len(m.form.labels) {
		label = m.form.labels[index]
	}
	if display == "" && selectFieldsWithDefault[label] {
		display = "默认"
	}
	if label == "Wire API" && display == "responses" {
		display = "responses (推荐)"
	}
	if display == "" {
		display = "未选择"
	}
	if index == m.form.focusIndex {
		return selectedStyle.Render("› " + display + "  [Enter 选择 · ←/→ 切换]")
	}
	return "› " + display + "  " + mutedStyle.Render("[Enter 选择 · ←/→ 切换]")
}

func help(key, desc string) string {
	return helpKeyStyle.Render("["+key+"]") + desc
}

func isSelectableRow(row listRow) bool {
	return row.kind == rowProvider || row.kind == rowAdd
}

func headingKey(app ccswitch.AppType) string {
	return "heading:" + app.String()
}

func providerKey(app ccswitch.AppType, id string) string {
	return "provider:" + app.String() + ":" + id
}

func addKey(app ccswitch.AppType) string {
	return "add:" + app.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m *Model) modeLabel() string {
	switch m.mode {
	case modeForm:
		if m.form.editMode {
			return "编辑"
		}
		return "新增"
	case modeConfirm:
		return "确认"
	case modeUpdateConfirm:
		if m.applyingUpdate {
			return "更新中"
		}
		return "发现更新"
	default:
		return "列表"
	}
}

func (m *Model) currentProviderName(app ccswitch.AppType) string {
	currentID := m.current[app]
	if currentID == "" {
		return ""
	}
	for _, provider := range m.providers[app] {
		if provider.ID == currentID {
			return provider.Name
		}
	}
	return currentID
}

func (m *Model) canDeleteCurrentConfirm() bool {
	if m.confirm == nil {
		return false
	}
	if m.current[m.confirm.app] != m.confirm.provider.ID {
		return true
	}
	return m.providerCount(m.confirm.app) == 1
}

func (m *Model) providerCount(app ccswitch.AppType) int {
	return len(m.providers[app])
}

func (m *Model) formHint() string {
	switch m.form.app {
	case ccswitch.AppGlobal:
		return "保存后按并集同步到各 CLI（只消费各自认识的字段），不会自动切换当前供应商"
	case ccswitch.AppClaude:
		return "写入 ~/.claude/settings.json（兼容旧版 claude.json）"
	case ccswitch.AppCodex:
		return "写入 ~/.codex/auth.json 与 ~/.codex/config.toml"
	case ccswitch.AppPi:
		return "写入 ~/.pi/agent/models.json、auth.json、settings.json（切换时设置 defaultProvider）"
	default:
		return "写入对应 CLI 的 live 配置"
	}
}

func placeholderFor(app ccswitch.AppType, label string) string {
	switch label {
	case "Name":
		return "例如 Official / 中转 / 公司内网"
	case "Base URL":
		switch app {
		case ccswitch.AppGlobal:
			return "例如 https://api.example.com 或带 /v1 的网关地址"
		case ccswitch.AppClaude:
			return "例如 https://api.anthropic.com"
		case ccswitch.AppCodex, ccswitch.AppPi:
			return "例如 https://api.openai.com/v1"
		}
	case "API Key":
		return "可留空以保留 OAuth / 登录态"
	case "Model":
		switch app {
		case ccswitch.AppGlobal:
			return "例如 gpt-5 / claude-sonnet / qwen3"
		case ccswitch.AppClaude:
			return "例如 claude-sonnet-4-5"
		case ccswitch.AppCodex:
			return "例如 gpt-5-codex"
		case ccswitch.AppPi:
			return "例如 mimo-v2.5-pro / gpt-5"
		}
	case "API Type":
		return "openai-completions / openai-responses / anthropic-messages / google-generative-ai"
	case "Context Window":
		return "例如 128000 / 200000；Codex 留空表示不写"
	case "Max Tokens":
		return "例如 8192 / 32000；留空表示不写"
	case "Reasoning":
		return "true / false"
	case "Reasoning Model":
		return "例如 claude-opus / gpt-5.4；对应 ANTHROPIC_REASONING_MODEL"
	case "Haiku/Fast Model":
		return "轻量模型；留空则与 Model 相同"
	case "Reasoning Effort":
		return "默认 / minimal / low / medium / high / xhigh"
	case "Reasoning Summary":
		return "默认 / auto / concise / detailed / none"
	case "Verbosity":
		return "默认 / low / medium / high"
	case "Service Tier":
		return "默认 / default / priority / flex / fast"
	case "Wire API":
		return "responses（Codex 已移除 chat 支持）"
	case "Max Tokens Field":
		return "默认 / max_tokens / max_completion_tokens"
	case "Supports Developer Role", "Supports Reasoning Effort", "Supports Usage In Streaming":
		return "默认 / true / false"
	case "Website":
		return "Optional: provider website"
	case "Notes":
		return "Optional: notes"
	}
	return label
}

func providerURLLines(store *ccswitch.Store, app ccswitch.AppType, provider ccswitch.Provider, width int) []string {
	input := store.ExtractInput(app, provider)
	baseURL := strings.TrimSpace(input.BaseURL)
	if baseURL == "" {
		baseURL = providerBaseURLFallback(app)
	}

	lines := wrapLabelValue("Base URL", baseURL, width)
	if website := strings.TrimSpace(input.Website); website != "" {
		lines = append(lines, wrapLabelValue("Website", website, width)...)
	}

	for index := range lines {
		lines[index] = mutedStyle.Render(lines[index])
	}
	return lines
}

func providerBaseURLFallback(app ccswitch.AppType) string {
	switch app {
	case ccswitch.AppClaude, ccswitch.AppCodex:
		return "官方登录"
	case ccswitch.AppPi:
		return "未设置 Base URL"
	default:
		return "-"
	}
}

func (m *Model) providerColumnWidths(isCurrent bool) (int, int) {
	total := max(36, m.width-6)
	statusWidth := 0
	if isCurrent {
		statusWidth = lipgloss.Width("当前") + 1
	}
	contentWidth := total - 4 - statusWidth
	if contentWidth < 20 {
		contentWidth = 20
	}
	nameWidth := contentWidth * 2 / 5
	if nameWidth < 12 {
		nameWidth = 12
	}
	if nameWidth > 28 {
		nameWidth = 28
	}
	endpointWidth := contentWidth - nameWidth - 1
	if endpointWidth < 12 {
		endpointWidth = 12
	}
	return nameWidth, endpointWidth
}

func wrapLabelValue(label, value string, width int) []string {
	prefix := label + ": "
	if width <= 0 {
		return nil
	}

	prefixWidth := lipgloss.Width(prefix)
	available := width - prefixWidth
	if available < 8 {
		lines := []string{truncate(prefix, width)}
		for _, line := range wrapText(value, max(4, width-2)) {
			lines = append(lines, "  "+line)
		}
		return lines
	}

	wrapped := wrapText(value, available)
	if len(wrapped) == 0 {
		return []string{prefix}
	}

	lines := []string{prefix + wrapped[0]}
	indent := strings.Repeat(" ", prefixWidth)
	for _, line := range wrapped[1:] {
		lines = append(lines, indent+line)
	}
	return lines
}

func wrapText(input string, width int) []string {
	if width <= 0 {
		return nil
	}
	if input == "" {
		return []string{""}
	}

	lines := make([]string, 0, 1)
	var builder strings.Builder
	used := 0

	for _, r := range input {
		runeWidth := lipgloss.Width(string(r))
		if runeWidth > width {
			runeWidth = width
		}
		if used+runeWidth > width && builder.Len() > 0 {
			lines = append(lines, builder.String())
			builder.Reset()
			used = 0
		}
		builder.WriteRune(r)
		used += runeWidth
	}

	if builder.Len() > 0 {
		lines = append(lines, builder.String())
	}
	return lines
}

func wrapInlineItems(items []string, width int) []string {
	if len(items) == 0 {
		return nil
	}

	lines := make([]string, 0, 2)
	current := ""
	for _, item := range items {
		next := item
		if current != "" {
			next = current + " " + item
		}
		if current != "" && lipgloss.Width(next) > width {
			lines = append(lines, current)
			current = item
			continue
		}
		current = next
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func padRight(input string, width int) string {
	gap := width - lipgloss.Width(input)
	if gap <= 0 {
		return input
	}
	return input + strings.Repeat(" ", gap)
}

func truncate(input string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if lipgloss.Width(input) <= limit {
		return input
	}
	if limit <= 1 {
		return "…"
	}

	var builder strings.Builder
	used := 0
	for _, r := range input {
		runeWidth := lipgloss.Width(string(r))
		if used+runeWidth+1 > limit {
			break
		}
		builder.WriteRune(r)
		used += runeWidth
	}

	if builder.Len() == 0 {
		return "…"
	}
	return builder.String() + "…"
}

func stringValueAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		if value == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(value))
	}
}
