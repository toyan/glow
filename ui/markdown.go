package ui

// markdown is a local markdown document shown in the pager.
type markdown struct {
	// Full path of the local markdown file. Empty for stdin.
	localPath string

	Body string
	Note string
}
