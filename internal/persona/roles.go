package persona

type Role string

const (
	RoleProjectManager Role = "project_manager"
	RoleTechLead       Role = "tech_lead"
	RoleFrontendDev    Role = "frontend_dev"
	RoleBackendDev     Role = "backend_dev"
	RoleUXDesigner     Role = "ux_designer"
	RoleQAEngineer     Role = "qa_engineer"
	RoleDevOps         Role = "devops_engineer"
	RoleResearcher     Role = "researcher"
)

var RoleNames = map[Role]string{
	RoleProjectManager: "Project Manager",
	RoleTechLead:       "Tech Lead",
	RoleFrontendDev:    "Frontend Developer",
	RoleBackendDev:     "Backend Developer",
	RoleUXDesigner:     "UX/UI Designer",
	RoleQAEngineer:     "QA Engineer",
	RoleDevOps:         "DevOps Engineer",
	RoleResearcher:     "Researcher",
}

type RoleSpec struct {
	Role        Role     `yaml:"role" json:"role"`
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description"`
	Specialties []string `yaml:"specialties" json:"specialties"`
}

var BuiltInRoles = map[Role]RoleSpec{
	RoleProjectManager: {
		Role:        RoleProjectManager,
		Name:        "Project Manager",
		Description: "Coordinates work, tracks progress, and manages project scope.",
		Specialties: []string{"task tracking", "dependency management", "timeline estimation"},
	},
	RoleTechLead: {
		Role:        RoleTechLead,
		Name:        "Tech Lead",
		Description: "Makes technical decisions, reviews code, and guides architecture.",
		Specialties: []string{"architecture", "code review", "technical decisions"},
	},
	RoleFrontendDev: {
		Role:        RoleFrontendDev,
		Name:        "Frontend Developer",
		Description: "Builds user interfaces and client-side functionality.",
		Specialties: []string{"UI development", "state management", "accessibility", "browser APIs"},
	},
	RoleBackendDev: {
		Role:        RoleBackendDev,
		Name:        "Backend Developer",
		Description: "Builds server-side logic, APIs, and data infrastructure.",
		Specialties: []string{"API design", "database", "server architecture", "performance"},
	},
	RoleUXDesigner: {
		Role:        RoleUXDesigner,
		Name:        "UX/UI Designer",
		Description: "Designs user experience and interface mockups.",
		Specialties: []string{"user research", "wireframing", "prototyping", "usability"},
	},
	RoleQAEngineer: {
		Role:        RoleQAEngineer,
		Name:        "QA Engineer",
		Description: "Tests software and tracks bugs.",
		Specialties: []string{"test automation", "bug triage", "test planning", "quality metrics"},
	},
	RoleDevOps: {
		Role:        RoleDevOps,
		Name:        "DevOps Engineer",
		Description: "Manages infrastructure, CI/CD, and deployment.",
		Specialties: []string{"CI/CD", "infrastructure", "monitoring", "deployment"},
	},
	RoleResearcher: {
		Role:        RoleResearcher,
		Name:        "Researcher",
		Description: "Investigates new technologies and evaluates solutions.",
		Specialties: []string{"technology evaluation", "proof of concept", "comparative analysis"},
	},
}

func Describe(r Role) RoleSpec {
	if spec, ok := BuiltInRoles[r]; ok {
		return spec
	}
	return RoleSpec{Role: r, Name: string(r), Description: "", Specialties: nil}
}

func AllRoles() []RoleSpec {
	result := make([]RoleSpec, 0, len(BuiltInRoles))
	for _, spec := range BuiltInRoles {
		result = append(result, spec)
	}
	return result
}

func IsValidRole(r string) bool {
	_, ok := BuiltInRoles[Role(r)]
	return ok
}

func KnowledgeScope(r Role) []string {
	switch r {
	case RoleProjectManager:
		return []string{"project_status", "timelines", "dependencies", "risks"}
	case RoleTechLead:
		return []string{"architecture", "code_review", "standards", "decisions"}
	case RoleFrontendDev:
		return []string{"ui", "components", "state", "accessibility", "browser_apis"}
	case RoleBackendDev:
		return []string{"api", "database", "server", "performance", "security"}
	case RoleUXDesigner:
		return []string{"user_research", "wireframes", "prototypes", "usability"}
	case RoleQAEngineer:
		return []string{"test_cases", "bugs", "test_automation", "quality_metrics"}
	case RoleDevOps:
		return []string{"ci_cd", "infrastructure", "monitoring", "deployment"}
	case RoleResearcher:
		return []string{"technology_evaluation", "poc", "comparative_analysis"}
	default:
		return []string{"general"}
	}
}
