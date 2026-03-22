package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Skill 技能数据结构
type Skill struct {
	ID              string
	Name            string
	Description     string
	MainInstruction string
	Scripts         []string
	References      []string
}

// Manager 技能管理器
type Manager struct {
	skillsDir string
	skills    []Skill
}

// NewManager 创建技能管理器
func NewManager() *Manager {
	skillsDir := filepath.Join(getWorkspaceDir(), ".awesome", "skills")
	return &Manager{
		skillsDir: skillsDir,
		skills:    make([]Skill, 0),
	}
}

// LoadAll 发现并加载所有技能元数据
func (m *Manager) LoadAll() error {
	if _, err := os.Stat(m.skillsDir); os.IsNotExist(err) {
		return nil
	}

	entries, err := os.ReadDir(m.skillsDir)
	if err != nil {
		return fmt.Errorf("failed to read skills directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillID := entry.Name()

		skillData, err := LoadSkill(skillID)
		if err != nil {
			fmt.Printf("warning: failed to load skill %s: %v\n", skillID, err)
			continue
		}

		m.skills = append(m.skills, skillData)
	}

	return nil
}

// FormatForPrompt 格式化技能元数据用于系统提示词
func (m *Manager) FormatForPrompt() string {
	if len(m.skills) == 0 {
		return "No skills available."
	}

	var sb strings.Builder
	sb.WriteString("You have access to the following skills. ")
	sb.WriteString("When a user request matches a skill's purpose, use the `load_skill` tool to load the full skill instructions.\n\n")

	for _, loadedSkill := range m.skills {
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n", loadedSkill.Name, loadedSkill.Description))
	}

	return sb.String()
}

func getWorkspaceDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}
