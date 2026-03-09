package botcmd

import (
	"fmt"
	"strings"

	"github.com/doeshing/nekoclaw/internal/persona"
)

const (
	CommandReset   = "reset"
	CommandPersona = "persona"

	OptionPersonaName = "name"

	ActionPersonaSelect = "persona_select"
	PersonaOffValue     = "__off__"

	MaxPersonaSelectOptions = 25
	MaxPersonaSelectMenus   = 5
	MaxPersonaSelectValues  = MaxPersonaSelectOptions * MaxPersonaSelectMenus
)

type CommandOption struct {
	Name        string
	Description string
	Required    bool
}

type CommandSpec struct {
	Name        string
	Description string
	Options     []CommandOption
}

type Invocation struct {
	Name            string
	StringOptions   map[string]string
	ComponentAction string
	ComponentValue  string
}

func (i Invocation) StringOption(name string) string {
	if len(i.StringOptions) == 0 {
		return ""
	}
	return strings.TrimSpace(i.StringOptions[name])
}

type Runtime interface {
	ResetSession() string
	ListPersonas() []persona.PersonaInfo
	ActivePersona() *persona.PersonaInfo
	FindPersonaByName(name string) (dirName string, found bool)
	SetActivePersona(dirName string) error
	ClearActivePersona() error
}

type Result struct {
	Message        string
	Selector       *PersonaSelector
	SelectorNotice string
	UpdateExisting bool
	Event          *Event
}

type Event struct {
	Type        string
	SessionID   string
	PersonaName string
}

const (
	EventReset          = "reset"
	EventPersonaSet     = "persona_set"
	EventPersonaCleared = "persona_cleared"
)

type PersonaSelector struct {
	Title    string
	Options  []PersonaOption
	OffLabel string
}

type PersonaOption struct {
	DirName     string
	Name        string
	Description string
	Active      bool
}

func Specs() []CommandSpec {
	return []CommandSpec{
		{
			Name:        CommandReset,
			Description: "重置對話，開始新的對話",
		},
		{
			Name:        CommandPersona,
			Description: "查看或切換角色",
			Options: []CommandOption{
				{
					Name:        OptionPersonaName,
					Description: "角色名稱；留空會顯示選單",
					Required:    false,
				},
			},
		},
	}
}

func Dispatch(runtime Runtime, inv Invocation) (Result, bool) {
	switch inv.Name {
	case CommandReset:
		return handleReset(runtime, inv), true
	case CommandPersona:
		return handlePersona(runtime, inv), true
	default:
		return Result{}, false
	}
}

func BuildPersonaSelector(personas []persona.PersonaInfo, active *persona.PersonaInfo) *PersonaSelector {
	if len(personas) == 0 {
		return nil
	}

	selector := &PersonaSelector{
		Title:    "📋 可用角色：",
		Options:  make([]PersonaOption, 0, len(personas)),
		OffLabel: "❌ 停用角色",
	}
	activeDir := ""
	if active != nil {
		activeDir = active.DirName
	}
	for _, p := range personas {
		selector.Options = append(selector.Options, PersonaOption{
			DirName:     p.DirName,
			Name:        p.Name,
			Description: p.Description,
			Active:      p.DirName == activeDir,
		})
	}
	return selector
}

func RenderPersonaSelectorText(selector *PersonaSelector, notice string) string {
	if selector == nil {
		return strings.TrimSpace(notice)
	}

	var sb strings.Builder
	notice = strings.TrimSpace(notice)
	if notice != "" {
		sb.WriteString(notice)
		sb.WriteString("\n\n")
	}

	title := strings.TrimSpace(selector.Title)
	if title == "" {
		title = "📋 可用角色："
	}
	sb.WriteString(title)
	sb.WriteString("\n")

	for _, option := range selector.Options {
		marker := "　"
		if option.Active {
			marker = "▶ "
		}
		sb.WriteString(marker)
		sb.WriteString(option.Name)
		if option.Description != "" {
			sb.WriteString(" — ")
			sb.WriteString(option.Description)
		}
		sb.WriteString("\n")
	}

	return strings.TrimRight(sb.String(), "\n")
}

func handleReset(runtime Runtime, inv Invocation) Result {
	sessionID := runtime.ResetSession()
	return Result{
		Message: "✅ 對話已重置，開始新的對話。",
		Event: &Event{
			Type:      EventReset,
			SessionID: sessionID,
		},
		UpdateExisting: inv.ComponentAction != "",
	}
}

func handlePersona(runtime Runtime, inv Invocation) Result {
	if inv.ComponentAction == ActionPersonaSelect {
		return handlePersonaSelection(runtime, inv.ComponentValue, true)
	}

	arg := inv.StringOption(OptionPersonaName)
	if arg == "" {
		personas := runtime.ListPersonas()
		if len(personas) == 0 {
			return Result{Message: "📋 目前沒有可用的角色。"}
		}
		return Result{
			Selector: BuildPersonaSelector(personas, runtime.ActivePersona()),
		}
	}

	return handlePersonaSelection(runtime, arg, false)
}

func handlePersonaSelection(runtime Runtime, raw string, updateExisting bool) Result {
	arg := strings.TrimSpace(raw)
	if arg == "" {
		return Result{
			Message:        "⚠️ 未指定角色名稱。",
			UpdateExisting: updateExisting,
		}
	}

	if arg == PersonaOffValue {
		arg = "off"
	}

	if isClearPersonaValue(arg) {
		if err := runtime.ClearActivePersona(); err != nil {
			return selectorErrorResult(runtime, "⚠️ "+err.Error(), updateExisting)
		}
		return selectorSuccessResult(runtime, "✅ 已停用角色。", updateExisting, &Event{
			Type: EventPersonaCleared,
		})
	}

	dirName, found := runtime.FindPersonaByName(arg)
	if !found {
		return Result{
			Message: fmt.Sprintf("⚠️ 找不到名為「%s」的角色。使用 /persona 查看可用清單。", arg),
		}
	}

	if err := runtime.SetActivePersona(dirName); err != nil {
		return selectorErrorResult(runtime, "⚠️ "+err.Error(), updateExisting)
	}

	name := dirName
	if active := runtime.ActivePersona(); active != nil {
		name = active.Name
	}

	return selectorSuccessResult(runtime, fmt.Sprintf("✅ 已切換為角色「%s」。", name), updateExisting, &Event{
		Type:        EventPersonaSet,
		PersonaName: name,
	})
}

func selectorSuccessResult(runtime Runtime, message string, updateExisting bool, event *Event) Result {
	result := Result{
		Message:        message,
		UpdateExisting: updateExisting,
		Event:          event,
	}
	if updateExisting {
		if selector := BuildPersonaSelector(runtime.ListPersonas(), runtime.ActivePersona()); selector != nil {
			result.Selector = selector
			result.SelectorNotice = message
		}
	}
	return result
}

func selectorErrorResult(runtime Runtime, message string, updateExisting bool) Result {
	result := Result{
		Message:        message,
		UpdateExisting: updateExisting,
	}
	if updateExisting {
		if selector := BuildPersonaSelector(runtime.ListPersonas(), runtime.ActivePersona()); selector != nil {
			result.Selector = selector
			result.SelectorNotice = message
		}
	}
	return result
}

func isClearPersonaValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "clear", "none":
		return true
	default:
		return false
	}
}
