package runtime

import (
	"sync"
	"time"

	"codergag/internal/reverse"
)

type LogTemplateStore struct {
	mu        sync.RWMutex
	templates map[string]*reverse.LogTemplate
	index     map[string][]string
}

func NewLogTemplateStore() *LogTemplateStore {
	return &LogTemplateStore{
		templates: make(map[string]*reverse.LogTemplate),
		index:     make(map[string][]string),
	}
}

func (s *LogTemplateStore) FindOrCreate(projectID, rawText string) (*reverse.LogTemplate, bool) {
	normalized := NormalizeLogTemplate(rawText)
	
	s.mu.RLock()
	key := projectID + ":" + normalized
	if existing, ok := s.templates[key]; ok {
		s.mu.RUnlock()
		return existing, false
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	key = projectID + ":" + normalized
	if existing, ok := s.templates[key]; ok {
		return existing, false
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	template := &reverse.LogTemplate{
		ID:        newID(),
		ProjectID: projectID,
		Template:  normalized,
		Count:     0,
		FirstSeen: now,
		LastSeen:  now,
	}
	s.templates[key] = template
	s.index[projectID] = append(s.index[projectID], template.ID)
	return template, true
}

func (s *LogTemplateStore) Increment(templateID string, sampleValue string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range s.templates {
		if t.ID == templateID {
			t.Count++
			t.LastSeen = time.Now().UTC().Format(time.RFC3339Nano)
			if sampleValue != "" && len(t.SampleVals) < 5 {
				t.SampleVals = append(t.SampleVals, sampleValue)
			}
			break
		}
	}
}

func (s *LogTemplateStore) Get(id string) *reverse.LogTemplate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.templates[id]
}

func (s *LogTemplateStore) GetByProject(projectID string) []*reverse.LogTemplate {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*reverse.LogTemplate
	for _, t := range s.templates {
		if t.ProjectID == projectID {
			result = append(result, t)
		}
	}
	return result
}

func (s *LogTemplateStore) LinkSource(templateID, sourceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range s.templates {
		if t.ID == templateID {
			for _, id := range t.SourceIDs {
				if id == sourceID {
					return
				}
			}
			t.SourceIDs = append(t.SourceIDs, sourceID)
			break
		}
	}
}

func (s *LogTemplateStore) LoadFromGraph(templates []*reverse.LogTemplate) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range templates {
		key := t.ProjectID + ":" + NormalizeLogTemplate(t.Template)
		s.templates[key] = t
		s.index[t.ProjectID] = append(s.index[t.ProjectID], t.ID)
	}
}

func newID() string {
	return time.Now().UTC().Format("nanoid")
}
