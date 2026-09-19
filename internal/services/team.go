package services

import (
	"fmt"
	"time"

	"codergag/internal/graph"
	"codergag/internal/persona"
)

type TeamService struct {
	graph graph.GraphRepository
	code  *CodeGraphService
}

func NewTeamService(g graph.GraphRepository) *TeamService {
	return &TeamService{
		graph: g,
		code:  NewCodeGraphService(g),
	}
}

type TaskStatus string

const (
	TaskStatusTodo    TaskStatus = "TODO"
	TaskStatusDoing   TaskStatus = "DOING"
	TaskStatusReview  TaskStatus = "REVIEW"
	TaskStatusDone    TaskStatus = "DONE"
	TaskStatusBlocked TaskStatus = "BLOCKED"
)

func (s *TeamService) CreateTeam(projectID, teamName, ownerAgent string, members []map[string]any) (map[string]any, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	team, err := s.graph.UpsertNode("Team", map[string]any{
		"project_id": projectID,
		"name":       teamName,
	}, map[string]any{
		"owner_agent":  ownerAgent,
		"created_at":   now,
		"updated_at":   now,
		"member_count": len(members),
	})
	if err != nil {
		return nil, err
	}

	for _, member := range members {
		roleStr, _ := member["role"].(string)
		if !persona.IsValidRole(roleStr) {
			return nil, fmt.Errorf("invalid role: %s", roleStr)
		}
		agentName, _ := member["agent"].(string)
		if agentName == "" {
			agentName = roleStr
		}
		_, err := s.graph.UpsertNode("Agent", map[string]any{
			"project_id": projectID,
			"team_id":    team.ID,
			"agent":      agentName,
		}, map[string]any{
			"role":        roleStr,
			"agent_name":  agentName,
			"name":        persona.Describe(persona.Role(roleStr)).Name,
			"description": persona.Describe(persona.Role(roleStr)).Description,
			"specialties": persona.KnowledgeScope(persona.Role(roleStr)),
			"joined_at":   now,
			"owner_agent": ownerAgent,
		})
		if err != nil {
			continue
		}
		s.graph.Link("HAS_MEMBER", team.ID, agentName, map[string]any{
			"role": roleStr,
		})
	}

	return map[string]any{
		"team_id":    team.ID,
		"name":       teamName,
		"project_id": projectID,
		"members":    len(members),
		"created_at": now,
	}, nil
}

func (s *TeamService) ListTeams(projectID string) ([]map[string]any, error) {
	teams, err := s.graph.FindNodes("Team", map[string]any{
		"project_id": projectID,
	})
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, t := range teams {
		members, _ := s.graph.Neighbors(t.ID, "HAS_MEMBER", graph.DirOut)
		memberList := make([]string, 0, len(members))
		for _, m := range members {
			if role, ok := m.Edge.Properties["role"].(string); ok {
				memberList = append(memberList, fmt.Sprintf("%s (%s)", m.Node.ID, role))
			} else {
				memberList = append(memberList, m.Node.ID)
			}
		}
		row := map[string]any{
			"team_id":      t.ID,
			"name":         t.Properties["name"],
			"owner":        t.Properties["owner_agent"],
			"member_count": len(members),
		}
		if len(memberList) > 0 {
			row["members"] = memberList
		}
		results = append(results, row)
	}
	return results, nil
}

func (s *TeamService) AssignTask(projectID, teamID, title, description string, role persona.Role, assignee string, priority string, dueDate string) (map[string]any, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	task, err := s.graph.UpsertNode("Task", map[string]any{
		"project_id": projectID,
		"team_id":    teamID,
	}, map[string]any{
		"title":       title,
		"description": description,
		"status":      string(TaskStatusTodo),
		"assignee":    assignee,
		"role":        string(role),
		"priority":    priority,
		"created_at":  now,
		"updated_at":  now,
	})
	if err != nil {
		return nil, err
	}
	if dueDate != "" {
		task.SetProperty("due_date", dueDate)
	}
	s.graph.Link("ASSIGNED_TO", teamID, task.ID, map[string]any{
		"role":     string(role),
		"assignee": assignee,
	})
	return map[string]any{
		"task_id":  task.ID,
		"title":    title,
		"status":   string(TaskStatusTodo),
		"assignee": assignee,
		"role":     string(role),
	}, nil
}

func (s *TeamService) UpdateTask(projectID, taskID, status string) (map[string]any, error) {
	task, err := s.graph.GetNode(taskID)
	if err != nil || task.Properties["project_id"] != projectID {
		return nil, &ServiceError{Message: "task is absent or belongs to another project"}
	}
	// Upsert (not SetProperty) so persistent repositories journal the change.
	if _, err := s.graph.UpsertNode(task.Kind, map[string]any{"id": task.ID}, map[string]any{
		"status":     status,
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return nil, err
	}
	return map[string]any{
		"task_id": task.ID,
		"status":  status,
	}, nil
}

func (s *TeamService) GetTasks(projectID, teamID string, status string) ([]map[string]any, error) {
	filters := map[string]any{"project_id": projectID}
	if teamID != "" {
		filters["team_id"] = teamID
	}
	tasks, err := s.graph.FindNodes("Task", filters)
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, t := range tasks {
		if status != "" {
			ts, _ := t.Properties["status"].(string)
			if ts != status {
				continue
			}
		}
		results = append(results, graph.Present(t))
	}
	return results, nil
}

func (s *TeamService) ShareKnowledge(projectID, fromAgent, toAgent, toRole string, title, content string, scope []string) (map[string]any, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	node, err := s.graph.UpsertNode("SharedKnowledge", map[string]any{
		"project_id": projectID,
		"from_agent": fromAgent,
		"to_agent":   toAgent,
		"to_role":    toRole,
	}, map[string]any{
		"title":     title,
		"content":   content,
		"scope":     scope,
		"shared_at": now,
		"version":   1,
	})
	if err != nil {
		return nil, err
	}
	if toAgent != "" {
		s.graph.Link("SHARED_WITH", node.ID, toAgent, map[string]any{
			"type": "agent",
		})
	}
	if toRole != "" {
		s.graph.Link("SHARED_WITH_ROLE", node.ID, toRole, map[string]any{
			"type": "role",
		})
	}
	s.graph.Link("SHARED_BY", node.ID, fromAgent, nil)
	return graph.Present(node), nil
}

func (s *TeamService) GetTeamContext(projectID, teamID string, scopes []string, limit int) (map[string]any, error) {
	if limit <= 0 {
		limit = 50
	}
	filters := map[string]any{"project_id": projectID}
	shared, err := s.graph.FindNodes("SharedKnowledge", filters)
	if err != nil {
		return nil, err
	}
	agents, _ := s.graph.FindNodes("Agent", map[string]any{"project_id": projectID, "team_id": teamID})
	agentMap := make(map[string]bool)
	roleMap := make(map[string]bool)
	for _, a := range agents {
		agentMap[a.Properties["agent"].(string)] = true
		if role, ok := a.Properties["role"].(string); ok {
			roleMap[role] = true
		}
	}

	var relevant []map[string]any
	scopeSet := make(map[string]bool)
	for _, sc := range scopes {
		scopeSet[sc] = true
	}
	for _, k := range shared {
		if len(relevant) >= limit {
			break
		}
		matchesScope := false
		if kScopes, ok := k.Properties["scope"].([]any); ok {
			for _, sc := range kScopes {
				if scopeSet[sc.(string)] {
					matchesScope = true
					break
				}
			}
		} else if len(scopeSet) == 0 {
			matchesScope = true
		}
		if !matchesScope {
			continue
		}
		relevant = append(relevant, graph.Present(k))
	}

	var memories []map[string]any
	memNodes, _ := s.graph.FindNodes("Memory", map[string]any{"project_id": projectID})
	for _, m := range memNodes {
		if archived, _ := m.Properties["archived"].(bool); archived {
			continue
		}
		memories = append(memories, graph.Present(m))
		if len(memories) >= limit {
			break
		}
	}

	var hypotheses []map[string]any
	hypNodes, _ := s.graph.FindNodes("Hypothesis", map[string]any{"project_id": projectID})
	for _, h := range hypNodes {
		hypotheses = append(hypotheses, graph.Present(h))
		if len(hypotheses) >= limit {
			break
		}
	}

	return map[string]any{
		"project_id":       projectID,
		"team_id":          teamID,
		"agents":           len(agents),
		"roles":            len(roleMap),
		"shared_knowledge": relevant,
		"memories":         len(memories),
		"hypotheses":       len(hypotheses),
		"scopes":           scopes,
	}, nil
}

func (s *TeamService) ListAgents(projectID, teamID string) ([]map[string]any, error) {
	filters := map[string]any{"project_id": projectID}
	if teamID != "" {
		filters["team_id"] = teamID
	}
	agents, err := s.graph.FindNodes("Agent", filters)
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, a := range agents {
		row := map[string]any{
			"agent":     a.Properties["agent"],
			"role":      a.Properties["role"],
			"name":      a.Properties["name"],
			"joined_at": a.Properties["joined_at"],
		}
		if d, ok := a.Properties["description"]; ok {
			row["description"] = d
		}
		results = append(results, row)
	}
	return results, nil
}

func (s *TeamService) ListRoles() []map[string]any {
	roles := persona.AllRoles()
	result := make([]map[string]any, len(roles))
	for i, r := range roles {
		result[i] = map[string]any{
			"role":            string(r.Role),
			"name":            r.Name,
			"description":     r.Description,
			"specialties":     r.Specialties,
			"knowledge_scope": persona.KnowledgeScope(r.Role),
		}
	}
	return result
}
