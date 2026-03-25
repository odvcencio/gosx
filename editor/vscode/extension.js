const vscode = require("vscode");
const { LanguageClient, TransportKind } = require("vscode-languageclient/node");

let client;

function activate(context) {
  const config = vscode.workspace.getConfiguration("gosx");
  const command = config.get("languageServer.path") || "gosx";
  const extraArgs = config.get("languageServer.args") || [];

  const serverOptions = {
    command,
    args: extraArgs.concat(["lsp"]),
    transport: TransportKind.stdio,
    options: {
      cwd: workspaceRoot(),
    },
  };

  const clientOptions = {
    documentSelector: [{ scheme: "file", language: "gosx" }],
    synchronize: {
      fileEvents: vscode.workspace.createFileSystemWatcher("**/*.gsx"),
    },
  };

  client = new LanguageClient(
    "gosx",
    "GoSX Language Server",
    serverOptions,
    clientOptions,
  );

  context.subscriptions.push(client.start());
}

function deactivate() {
  if (!client) {
    return undefined;
  }
  return client.stop();
}

function workspaceRoot() {
  const folder = vscode.workspace.workspaceFolders && vscode.workspace.workspaceFolders[0];
  if (!folder) {
    return process.cwd();
  }
  return folder.uri.fsPath;
}

module.exports = {
  activate,
  deactivate,
};
