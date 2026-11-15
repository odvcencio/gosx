package route

func helperFileModuleHereSource() string {
	return FileModuleHere(FileModuleOptions{}).Source
}

func helperMustRegisterFileModuleHere(registry *FileModuleRegistry) {
	registry.RegisterHere(FileModuleOptions{})
}

func helperMustRegisterFileModuleViaWrapper(registry *FileModuleRegistry) {
	helperRegisterModuleWrapper(registry)
}

func helperRegisterModuleWrapper(registry *FileModuleRegistry) {
	registry.RegisterCaller(1, FileModuleOptions{})
}

func helperDirModuleHereSource() string {
	return DirModuleHere(DirModuleOptions{}).Source
}

func helperMustRegisterDirModuleViaWrapper(registry *DirModuleRegistry) {
	helperRegisterDirModuleWrapper(registry)
}

func helperRegisterDirModuleWrapper(registry *DirModuleRegistry) {
	registry.RegisterCaller(1, DirModuleOptions{})
}
