//go:build windows && (amd64 || arm64)

package desktop

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

func (a *windowsApp) RegisterProtocol(scheme string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	plan, err := BuildProtocolRegistration(ProtocolRegistration{
		Scheme:     scheme,
		AppID:      a.options.AppID,
		AppName:    a.options.Title,
		Executable: exe,
		Icon:       exe,
	})
	if err != nil {
		return err
	}
	return applyRegistryPlan(plan)
}

func (a *windowsApp) RegisterFileType(ext, icon, handler string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	plan, err := BuildFileAssociation(FileAssociationRegistration{
		Extension:   ext,
		AppID:       a.options.AppID,
		AppName:     a.options.Title,
		Description: handler,
		Executable:  exe,
		Icon:        icon,
	})
	if err != nil {
		return err
	}
	return applyRegistryPlan(plan)
}

func applyRegistryPlan(plan RegistryPlan) error {
	for _, value := range plan.Values {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, value.Key, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("create registry key HKCU\\%s: %w", value.Key, err)
		}
		if err := key.SetStringValue(value.Name, value.Value); err != nil {
			key.Close()
			return fmt.Errorf("write registry value HKCU\\%s[%q]: %w",
				value.Key, value.Name, err)
		}
		if err := key.Close(); err != nil {
			return fmt.Errorf("close registry key HKCU\\%s: %w", value.Key, err)
		}
	}
	return nil
}
