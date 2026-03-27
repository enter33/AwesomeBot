package tool

import (
	"log"
	"os/exec"
)

// checkDockerAvailable 检查 docker 是否可用
func checkDockerAvailable() bool {
	cmd := exec.Command("docker", "ps")
	return cmd.Run() == nil
}

// CreateBashTool 创建 bash 工具，自动选择 DockerBashTool（如果 docker 可用）或常规 BashTool
func CreateBashTool(workspaceDir string) Tool {
	if !checkDockerAvailable() {
		// log.Printf("Docker not available, using regular bash tool")
		if workspaceDir != "" {
			return NewBashToolWithWorkspace(workspaceDir)
		}
		return NewBashTool()
	}
	if workspaceDir == "" {
		log.Printf("Docker available but workspace dir is empty, using regular bash tool")
		return NewBashTool()
	}
	containerName := generateContainerName(workspaceDir)
	log.Printf("Docker available, using DockerBashTool with sandbox container '%s'", containerName)
	return NewDockerBashTool(containerName, workspaceDir)
}
