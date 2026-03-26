package route

func helperFileModuleHereSource() string {
	return FileModuleHere(FileModuleOptions{}).Source
}

func helperMustRegisterFileModuleHere() {
	MustRegisterFileModuleHere(FileModuleOptions{})
}

func helperMustRegisterFileModuleViaWrapper() {
	helperRegisterModuleWrapper()
}

func helperRegisterModuleWrapper() {
	MustRegisterFileModuleCaller(1, FileModuleOptions{})
}

func helperDirModuleHereSource() string {
	return DirModuleHere(DirModuleOptions{}).Source
}

func helperMustRegisterDirModuleViaWrapper() {
	helperRegisterDirModuleWrapper()
}

func helperRegisterDirModuleWrapper() {
	MustRegisterDirModuleCaller(1, DirModuleOptions{})
}
