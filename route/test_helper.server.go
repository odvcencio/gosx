package route

func helperFileModuleHereSource() string {
	return FileModuleHere(FileModuleOptions{}).Source
}

func helperMustRegisterFileModuleHere(registry *FileModuleRegistry) {
	registry.MustRegisterHere(FileModuleOptions{})
}

func helperMustRegisterFileModuleViaWrapper(registry *FileModuleRegistry) {
	helperRegisterModuleWrapper(registry)
}

func helperRegisterModuleWrapper(registry *FileModuleRegistry) {
	registry.MustRegisterCaller(1, FileModuleOptions{})
}

func helperDirModuleHereSource() string {
	return DirModuleHere(DirModuleOptions{}).Source
}

func helperMustRegisterDirModuleViaWrapper(registry *DirModuleRegistry) {
	helperRegisterDirModuleWrapper(registry)
}

func helperRegisterDirModuleWrapper(registry *DirModuleRegistry) {
	registry.MustRegisterCaller(1, DirModuleOptions{})
}
