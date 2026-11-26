package docker

// Labels Docker Compose sets on the containers it creates.
const (
	ComposeProjectLabel    = "com.docker.compose.project"
	ComposeServiceLabel    = "com.docker.compose.service"
	ComposeOneoffLabel     = "com.docker.compose.oneoff"
	ComposeWorkingDirLabel = ComposeProjectLabel + ".working_dir"
	ComposeConfigLabel     = ComposeProjectLabel + ".config_files"
)
