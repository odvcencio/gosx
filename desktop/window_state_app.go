package desktop

import (
	"errors"
	"fmt"
	"io/fs"
	"runtime"
)

func prepareWindowState(options *Options) (*windowStatePersister, error) {
	if options.WindowState == nil {
		return nil, nil
	}
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("%w: window-state persistence is currently supported only on Windows", ErrUnsupported)
	}

	stateOptions := options.WindowState
	path, err := resolveWindowStatePath(stateOptions.Path, options.AppID)
	if err != nil {
		if stateOptions.OnError != nil {
			stateOptions.OnError(err)
		}
		return nil, nil
	}
	if saved, loadErr := LoadWindowState(path); loadErr == nil {
		options.InitialPlacement = saved
		options.savedWindowPlacement = &saved
	} else if !errors.Is(loadErr, fs.ErrNotExist) && stateOptions.OnError != nil {
		stateOptions.OnError(loadErr)
	}

	persister := newWindowStatePersister(path, stateOptions.Debounce, stateOptions.OnError)
	options.onPlacementChanged = persister.schedule
	userBeforeClose := options.OnBeforeClose
	options.OnBeforeClose = func(placement WindowPlacement) {
		_ = persister.flush(placement)
		if userBeforeClose != nil {
			userBeforeClose(placement)
		}
	}
	return persister, nil
}
