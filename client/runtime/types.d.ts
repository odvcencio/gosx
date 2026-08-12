// Browser ABI declarations used only by the strict generated contract
// check. Product modules still install these values at runtime; this file does
// not emit JavaScript or create browser globals.
interface Window {
  [name: string]: unknown;
  __gosx_runtime_contract?: unknown;
  __gosx_runtime_abi_support?: unknown;
  __gosx_runtime_mailbox?: unknown;
  __gosx_runtime_wasm_loader?: unknown;
}
