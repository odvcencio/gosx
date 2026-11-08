//go:build linux && (amd64 || arm64)

package desktop

import (
	"fmt"
	"github.com/ebitengine/purego"
	"sync"
)

type gtkAPI struct {
	gtkInitCheck               func(uintptr, uintptr) int32
	gtkWindowNew               func(int32) uintptr
	gtkWindowSetTitle          func(uintptr, string)
	gtkWindowSetDefaultSize    func(uintptr, int32, int32)
	gtkWindowPresent           func(uintptr)
	gtkWindowIconify           func(uintptr)
	gtkWindowMaximize          func(uintptr)
	gtkWindowUnmaximize        func(uintptr)
	gtkWindowFullscreen        func(uintptr)
	gtkWindowUnfullscreen      func(uintptr)
	gtkContainerAdd            func(uintptr, uintptr)
	gtkBoxNew                  func(int32, int32) uintptr
	gtkBoxPackStart            func(uintptr, uintptr, int32, int32, uint32)
	gtkBoxReorderChild         func(uintptr, uintptr, int32)
	gtkMenuBarNew              func() uintptr
	gtkMenuNew                 func() uintptr
	gtkMenuItemNew             func(string) uintptr
	gtkCheckMenuItemNew        func(string) uintptr
	gtkCheckMenuItemSetActive  func(uintptr, int32)
	gtkSeparatorMenuItemNew    func() uintptr
	gtkMenuItemSetSubmenu      func(uintptr, uintptr)
	gtkMenuShellAppend         func(uintptr, uintptr)
	gtkWidgetSetSensitive      func(uintptr, int32)
	gtkWidgetSetSizeRequest    func(uintptr, int32, int32)
	gtkFileChooserNativeNew    func(string, uintptr, int32, string, string) uintptr
	gtkNativeDialogRun         func(uintptr) int32
	gtkNativeDialogDestroy     func(uintptr)
	gtkFileChooserSetFolder    func(uintptr, string) int32
	gtkFileChooserSetName      func(uintptr, string)
	gtkFileChooserSetFilename  func(uintptr, string) int32
	gtkFileChooserSetMultiple  func(uintptr, int32)
	gtkFileChooserSetOverwrite func(uintptr, int32)
	gtkFileChooserFilename     func(uintptr) uintptr
	gtkFileChooserFilenames    func(uintptr) uintptr
	gtkFileFilterNew           func() uintptr
	gtkFileFilterSetName       func(uintptr, string)
	gtkFileFilterAddPattern    func(uintptr, string)
	gtkFileChooserAddFilter    func(uintptr, uintptr)
	slistFree                  func(uintptr)
	gtkWidgetShowAll           func(uintptr)
	gtkWidgetDestroy           func(uintptr)
	gtkMain                    func()
	gtkMainQuit                func()
	signalConnect              func(uintptr, string, uintptr, uintptr, uintptr, uint32) uintptr
	idleAdd                    func(int32, uintptr, uintptr, uintptr) uint32
	free                       func(uintptr)
	strlen                     func(uintptr) uintptr
	objectUnref                func(uintptr)
	objectRef                  func(uintptr) uintptr
	websiteDataNew             func(string, string, string, string, uintptr) uintptr
	contextNewWithData         func(uintptr) uintptr
	viewNewWithContext         func(uintptr) uintptr
	viewGetManager             func(uintptr) uintptr
	managerNew                 func() uintptr
	managerRegister            func(uintptr, string) int32
	managerAddScript           func(uintptr, uintptr)
	managerRemoveScripts       func(uintptr)
	scriptNew                  func(string, int32, int32, uintptr, uintptr) uintptr
	scriptUnref                func(uintptr)
	viewNew                    func(uintptr) uintptr
	viewLoadURI                func(uintptr, string)
	viewLoadHTML               func(uintptr, string, uintptr)
	viewReload                 func(uintptr)
	viewEvaluate               func(uintptr, string, int64, uintptr, uintptr, uintptr, uintptr, uintptr)
	viewSetMuted               func(uintptr, int32)
	resultValue                func(uintptr) uintptr
	valueIsString              func(uintptr) int32
	valueString                func(uintptr) uintptr
}

var linuxGTK struct {
	once sync.Once
	api  gtkAPI
	err  error
}

func loadGTK() (*gtkAPI, error) {
	linuxGTK.once.Do(func() {
		defer func() {
			if x := recover(); x != nil {
				linuxGTK.err = fmt.Errorf("%w: WebKitGTK symbol: %v", ErrUnsupported, x)
			}
		}()
		open := func(name string) uintptr {
			h, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
			if err != nil {
				panic(fmt.Sprintf("%s: %v", name, err))
			}
			return h
		}
		gtk := open("libgtk-3.so.0")
		obj := open("libgobject-2.0.so.0")
		glib := open("libglib-2.0.so.0")
		libc := open("libc.so.6")
		jsc := open("libjavascriptcoregtk-4.1.so.0")
		webkit := open("libwebkit2gtk-4.1.so.0")
		a := &linuxGTK.api
		bind := func(dst any, lib uintptr, symbol string) { purego.RegisterLibFunc(dst, lib, symbol) }
		bind(&a.gtkInitCheck, gtk, "gtk_init_check")
		bind(&a.gtkWindowNew, gtk, "gtk_window_new")
		bind(&a.gtkWindowSetTitle, gtk, "gtk_window_set_title")
		bind(&a.gtkWindowSetDefaultSize, gtk, "gtk_window_set_default_size")
		bind(&a.gtkWindowPresent, gtk, "gtk_window_present")
		bind(&a.gtkWindowIconify, gtk, "gtk_window_iconify")
		bind(&a.gtkWindowMaximize, gtk, "gtk_window_maximize")
		bind(&a.gtkWindowUnmaximize, gtk, "gtk_window_unmaximize")
		bind(&a.gtkWindowFullscreen, gtk, "gtk_window_fullscreen")
		bind(&a.gtkWindowUnfullscreen, gtk, "gtk_window_unfullscreen")
		bind(&a.gtkContainerAdd, gtk, "gtk_container_add")
		bind(&a.gtkBoxNew, gtk, "gtk_box_new")
		bind(&a.gtkBoxPackStart, gtk, "gtk_box_pack_start")
		bind(&a.gtkBoxReorderChild, gtk, "gtk_box_reorder_child")
		bind(&a.gtkMenuBarNew, gtk, "gtk_menu_bar_new")
		bind(&a.gtkMenuNew, gtk, "gtk_menu_new")
		bind(&a.gtkMenuItemNew, gtk, "gtk_menu_item_new_with_mnemonic")
		bind(&a.gtkCheckMenuItemNew, gtk, "gtk_check_menu_item_new_with_mnemonic")
		bind(&a.gtkCheckMenuItemSetActive, gtk, "gtk_check_menu_item_set_active")
		bind(&a.gtkSeparatorMenuItemNew, gtk, "gtk_separator_menu_item_new")
		bind(&a.gtkMenuItemSetSubmenu, gtk, "gtk_menu_item_set_submenu")
		bind(&a.gtkMenuShellAppend, gtk, "gtk_menu_shell_append")
		bind(&a.gtkWidgetSetSensitive, gtk, "gtk_widget_set_sensitive")
		bind(&a.gtkWidgetSetSizeRequest, gtk, "gtk_widget_set_size_request")
		bind(&a.gtkFileChooserNativeNew, gtk, "gtk_file_chooser_native_new")
		bind(&a.gtkNativeDialogRun, gtk, "gtk_native_dialog_run")
		bind(&a.gtkNativeDialogDestroy, gtk, "gtk_native_dialog_destroy")
		bind(&a.gtkFileChooserSetFolder, gtk, "gtk_file_chooser_set_current_folder")
		bind(&a.gtkFileChooserSetName, gtk, "gtk_file_chooser_set_current_name")
		bind(&a.gtkFileChooserSetFilename, gtk, "gtk_file_chooser_set_filename")
		bind(&a.gtkFileChooserSetMultiple, gtk, "gtk_file_chooser_set_select_multiple")
		bind(&a.gtkFileChooserSetOverwrite, gtk, "gtk_file_chooser_set_do_overwrite_confirmation")
		bind(&a.gtkFileChooserFilename, gtk, "gtk_file_chooser_get_filename")
		bind(&a.gtkFileChooserFilenames, gtk, "gtk_file_chooser_get_filenames")
		bind(&a.gtkFileFilterNew, gtk, "gtk_file_filter_new")
		bind(&a.gtkFileFilterSetName, gtk, "gtk_file_filter_set_name")
		bind(&a.gtkFileFilterAddPattern, gtk, "gtk_file_filter_add_pattern")
		bind(&a.gtkFileChooserAddFilter, gtk, "gtk_file_chooser_add_filter")
		bind(&a.gtkWidgetShowAll, gtk, "gtk_widget_show_all")
		bind(&a.gtkWidgetDestroy, gtk, "gtk_widget_destroy")
		bind(&a.gtkMain, gtk, "gtk_main")
		bind(&a.gtkMainQuit, gtk, "gtk_main_quit")
		bind(&a.signalConnect, obj, "g_signal_connect_data")
		bind(&a.objectUnref, obj, "g_object_unref")
		bind(&a.objectRef, obj, "g_object_ref")
		bind(&a.idleAdd, glib, "g_idle_add_full")
		bind(&a.free, glib, "g_free")
		bind(&a.slistFree, glib, "g_slist_free")
		bind(&a.strlen, libc, "strlen")
		bind(&a.websiteDataNew, webkit, "webkit_website_data_manager_new")
		bind(&a.contextNewWithData, webkit, "webkit_web_context_new_with_website_data_manager")
		bind(&a.viewNewWithContext, webkit, "webkit_web_view_new_with_context")
		bind(&a.viewGetManager, webkit, "webkit_web_view_get_user_content_manager")
		bind(&a.managerNew, webkit, "webkit_user_content_manager_new")
		bind(&a.managerRegister, webkit, "webkit_user_content_manager_register_script_message_handler")
		bind(&a.managerAddScript, webkit, "webkit_user_content_manager_add_script")
		bind(&a.managerRemoveScripts, webkit, "webkit_user_content_manager_remove_all_scripts")
		bind(&a.scriptNew, webkit, "webkit_user_script_new")
		bind(&a.scriptUnref, webkit, "webkit_user_script_unref")
		bind(&a.viewNew, webkit, "webkit_web_view_new_with_user_content_manager")
		bind(&a.viewLoadURI, webkit, "webkit_web_view_load_uri")
		bind(&a.viewLoadHTML, webkit, "webkit_web_view_load_html")
		bind(&a.viewReload, webkit, "webkit_web_view_reload")
		bind(&a.viewEvaluate, webkit, "webkit_web_view_evaluate_javascript")
		bind(&a.viewSetMuted, webkit, "webkit_web_view_set_is_muted")
		bind(&a.resultValue, webkit, "webkit_javascript_result_get_js_value")
		bind(&a.valueIsString, jsc, "jsc_value_is_string")
		bind(&a.valueString, jsc, "jsc_value_to_string")
	})
	if linuxGTK.err != nil {
		return nil, linuxGTK.err
	}
	return &linuxGTK.api, nil
}
