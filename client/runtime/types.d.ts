// Browser ABI declarations used only by the strict generated contract check.
// Product modules install one namespaced facade at runtime; this file emits no
// JavaScript and does not create browser globals.
interface GoSXRuntimeFacade {
  support?: unknown;
  mailbox?: unknown;
  loader?: unknown;
  abi?: unknown;
}

interface GoSXHostFacade {
  compatibility?: unknown;
  patch?: unknown;
}

interface GoSXBrowserFacade {
  runtime?: GoSXRuntimeFacade;
  host?: GoSXHostFacade;
  [name: string]: unknown;
}

interface Window {
  [name: string]: unknown;
  __gosx?: GoSXBrowserFacade;
  __gosx_runtime_contract?: unknown;
}
