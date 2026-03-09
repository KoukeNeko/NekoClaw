package botcmd

import (
	"errors"
	"testing"

	"github.com/doeshing/nekoclaw/internal/persona"
)

type stubRuntime struct {
	sessionID string
	personas  []persona.PersonaInfo
	active    *persona.PersonaInfo
	resetHits int
	clearHits int
	setHits   []string
	setErr    error
	clearErr  error
}

func (r *stubRuntime) ResetSession() string {
	r.resetHits++
	return r.sessionID
}

func (r *stubRuntime) ListPersonas() []persona.PersonaInfo {
	return append([]persona.PersonaInfo(nil), r.personas...)
}

func (r *stubRuntime) ActivePersona() *persona.PersonaInfo {
	if r.active == nil {
		return nil
	}
	cp := *r.active
	return &cp
}

func (r *stubRuntime) FindPersonaByName(name string) (string, bool) {
	switch name {
	case "hero", "Hero":
		return "hero", true
	case "mage":
		return "mage", true
	case "ero":
		return "hero", true
	default:
		return "", false
	}
}

func (r *stubRuntime) SetActivePersona(dirName string) error {
	if r.setErr != nil {
		return r.setErr
	}
	r.setHits = append(r.setHits, dirName)
	for _, p := range r.personas {
		if p.DirName == dirName {
			cp := p
			r.active = &cp
			return nil
		}
	}
	r.active = &persona.PersonaInfo{DirName: dirName, Name: dirName}
	return nil
}

func (r *stubRuntime) ClearActivePersona() error {
	if r.clearErr != nil {
		return r.clearErr
	}
	r.clearHits++
	r.active = nil
	return nil
}

func TestDispatchReset(t *testing.T) {
	runtime := &stubRuntime{sessionID: "discord:chan-1-0101-000000"}

	result, handled := Dispatch(runtime, Invocation{Name: CommandReset})
	if !handled {
		t.Fatal("expected reset command to be handled")
	}
	if runtime.resetHits != 1 {
		t.Fatalf("expected reset hits = 1, got %d", runtime.resetHits)
	}
	if result.Message != "✅ 對話已重置，開始新的對話。" {
		t.Fatalf("unexpected reset message: %q", result.Message)
	}
	if result.Event == nil || result.Event.Type != EventReset || result.Event.SessionID != runtime.sessionID {
		t.Fatalf("unexpected reset event: %#v", result.Event)
	}
}

func TestDispatchPersonaSelector(t *testing.T) {
	active := persona.PersonaInfo{DirName: "hero", Name: "Hero", Description: "Brave cat"}
	runtime := &stubRuntime{
		personas: []persona.PersonaInfo{
			active,
			{DirName: "mage", Name: "Mage", Description: "Arcane cat"},
		},
		active: &active,
	}

	result, handled := Dispatch(runtime, Invocation{Name: CommandPersona})
	if !handled {
		t.Fatal("expected persona command to be handled")
	}
	if result.Selector == nil {
		t.Fatal("expected selector for bare persona command")
	}
	if len(result.Selector.Options) != 2 {
		t.Fatalf("expected 2 selector options, got %d", len(result.Selector.Options))
	}
	if !result.Selector.Options[0].Active {
		t.Fatalf("expected first persona to be active: %#v", result.Selector.Options[0])
	}
}

func TestDispatchPersonaSwitchBySubstring(t *testing.T) {
	runtime := &stubRuntime{
		personas: []persona.PersonaInfo{
			{DirName: "hero", Name: "Hero"},
			{DirName: "mage", Name: "Mage"},
		},
	}

	result, handled := Dispatch(runtime, Invocation{
		Name: CommandPersona,
		StringOptions: map[string]string{
			OptionPersonaName: "ero",
		},
	})
	if !handled {
		t.Fatal("expected persona switch to be handled")
	}
	if len(runtime.setHits) != 1 || runtime.setHits[0] != "hero" {
		t.Fatalf("unexpected set hits: %#v", runtime.setHits)
	}
	if result.Message != "✅ 已切換為角色「Hero」。" {
		t.Fatalf("unexpected switch message: %q", result.Message)
	}
	if result.Event == nil || result.Event.Type != EventPersonaSet || result.Event.PersonaName != "Hero" {
		t.Fatalf("unexpected switch event: %#v", result.Event)
	}
}

func TestDispatchPersonaOffViaComponentUpdatesSelector(t *testing.T) {
	active := persona.PersonaInfo{DirName: "hero", Name: "Hero"}
	runtime := &stubRuntime{
		personas: []persona.PersonaInfo{
			active,
			{DirName: "mage", Name: "Mage"},
		},
		active: &active,
	}

	result, handled := Dispatch(runtime, Invocation{
		Name:            CommandPersona,
		ComponentAction: ActionPersonaSelect,
		ComponentValue:  PersonaOffValue,
	})
	if !handled {
		t.Fatal("expected persona component action to be handled")
	}
	if runtime.clearHits != 1 {
		t.Fatalf("expected clear hits = 1, got %d", runtime.clearHits)
	}
	if !result.UpdateExisting {
		t.Fatal("expected component action to update existing selector")
	}
	if result.Selector == nil {
		t.Fatal("expected selector to be returned for component update")
	}
	if result.SelectorNotice != "✅ 已停用角色。" {
		t.Fatalf("unexpected selector notice: %q", result.SelectorNotice)
	}
}

func TestDispatchPersonaSetErrorReturnsSelectorForComponent(t *testing.T) {
	active := persona.PersonaInfo{DirName: "hero", Name: "Hero"}
	runtime := &stubRuntime{
		personas: []persona.PersonaInfo{
			active,
		},
		active: &active,
		setErr: errors.New("boom"),
	}

	result, handled := Dispatch(runtime, Invocation{
		Name:            CommandPersona,
		ComponentAction: ActionPersonaSelect,
		ComponentValue:  "hero",
	})
	if !handled {
		t.Fatal("expected persona component action to be handled")
	}
	if result.Message != "⚠️ boom" {
		t.Fatalf("unexpected error message: %q", result.Message)
	}
	if result.Selector == nil {
		t.Fatal("expected selector to be preserved on component error")
	}
}
