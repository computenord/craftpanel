package mc

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/computenord/craftpanel/internal/fsutil"
)

var templateIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,39}$`)

// ServerTemplate is a reusable create blueprint.
type ServerTemplate struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Version   string `json:"version"`
	MemoryMB  int    `json:"memoryMB"`
	Notes     string `json:"notes,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

func (m *Manager) templatesPath() string {
	return filepath.Join(m.dataDir, "templates.json")
}

func (m *Manager) loadTemplates() []ServerTemplate {
	var list []ServerTemplate
	_ = fsutil.ReadJSON(m.templatesPath(), &list)
	if list == nil {
		list = []ServerTemplate{}
	}
	return list
}

func (m *Manager) saveTemplates(list []ServerTemplate) error {
	return fsutil.WriteJSONAtomic(m.templatesPath(), list)
}

func (m *Manager) ListTemplates() []ServerTemplate {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadTemplates()
}

func (m *Manager) SaveTemplate(t ServerTemplate) (ServerTemplate, error) {
	t.ID = strings.ToLower(strings.TrimSpace(t.ID))
	t.Name = strings.TrimSpace(t.Name)
	if !templateIDRe.MatchString(t.ID) {
		return ServerTemplate{}, errors.New("id must be 2-40 chars [a-z0-9_-]")
	}
	if t.Name == "" {
		return ServerTemplate{}, errors.New("name is required")
	}
	if !validServerType(t.Type) {
		return ServerTemplate{}, errors.New("invalid server type")
	}
	if t.Version == "" {
		return ServerTemplate{}, errors.New("version is required")
	}
	if t.MemoryMB == 0 {
		t.MemoryMB = 2048
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.loadTemplates()
	found := false
	for i := range list {
		if list[i].ID == t.ID {
			t.CreatedAt = list[i].CreatedAt
			list[i] = t
			found = true
			break
		}
	}
	if !found {
		t.CreatedAt = time.Now().UTC()
		list = append(list, t)
	}
	if err := m.saveTemplates(list); err != nil {
		return ServerTemplate{}, err
	}
	return t, nil
}

func (m *Manager) DeleteTemplate(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.loadTemplates()
	for i, t := range list {
		if t.ID == id {
			list = append(list[:i], list[i+1:]...)
			return m.saveTemplates(list)
		}
	}
	return errors.New("template not found")
}

func (m *Manager) GetTemplate(id string) (ServerTemplate, bool) {
	for _, t := range m.ListTemplates() {
		if t.ID == id {
			return t, true
		}
	}
	return ServerTemplate{}, false
}

// CreateFromTemplate builds a CreateRequest from a template and creates the server.
func (m *Manager) CreateFromTemplate(templateID, name string, port int) (ServerView, error) {
	t, ok := m.GetTemplate(templateID)
	if !ok {
		return ServerView{}, fmt.Errorf("template %q not found", templateID)
	}
	if strings.TrimSpace(name) == "" {
		name = t.Name
	}
	return m.Create(CreateRequest{
		Name: name, Type: t.Type, Version: t.Version,
		MemoryMB: t.MemoryMB, Port: port,
	})
}