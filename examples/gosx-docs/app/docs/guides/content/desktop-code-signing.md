# Sign Windows desktop packages with Azure Artifact Signing

`gosx desktop package --sign-provider azure-artifact-signing` signs every staged
PE file, the standalone uninstaller, and the per-user Setup executable with your
Azure certificate profile. Add `--verify-signatures` to require valid
Authenticode signatures before the package is written to the output directory.

Azure Artifact Signing, formerly Azure Trusted Signing, manages code-signing
certificates and private keys in Azure. Your package configuration contains only
non-secret settings. Supply credentials through your environment or Azure login.

The packager signs staged files before creating the portable ZIP, signs the
uninstaller before adding it to the Setup payload, and signs Setup after attaching
the payload. It then hashes the final artifacts and writes `latest.json`.
Authenticode signing is separate from the existing Ed25519 update-manifest
signature: keep using `--manifest-key` or `--manifest-sign-cmd` for `latest.json`.

## Set up Azure once

Follow Microsoft's [Artifact Signing setup guide](https://learn.microsoft.com/en-us/azure/artifact-signing/quickstart):

1. Create an Artifact Signing account in a supported region.
2. Complete identity validation and create a Public Trust certificate profile
   for publicly distributed desktop software.
3. Grant the signing identity the Artifact Signing Certificate Profile Signer
   role on the appropriate profile.
4. Copy the region endpoint, account name, and profile name from Azure. The
   endpoint must match the account's region.
5. Provision your signing host and authenticate it. Use the
   [Microsoft signing integration guide](https://learn.microsoft.com/en-us/azure/artifact-signing/how-to-signing-integrations)
   for Windows and the [jsign documentation](https://ebourg.github.io/jsign/)
   for other hosts.

All names and paths below are examples. Replace `https://signing.example.com`
with the HTTPS regional endpoint from your Azure account, such as the
`https://<region>.codesigning.azure.net` pattern. Replace `example-account` and
`example-profile` with your own non-secret identifiers.

## Configure the provider

Select the provider explicitly with `--sign-provider azure-artifact-signing`.
It conflicts with `--sign-cmd`; existing shell command templates keep their
behavior. For each setting, the provider uses the first non-empty value from the
flag, environment, then the optional `signing` block in package JSON.

| Setting | Flag | Environment variable | `signing` key |
| --- | --- | --- | --- |
| HTTPS endpoint | `--sign-endpoint` | `GOSX_ARTIFACT_SIGNING_ENDPOINT` | `endpoint` |
| Account name | `--sign-account` | `GOSX_ARTIFACT_SIGNING_ACCOUNT` | `account` |
| Certificate profile | `--sign-profile` | `GOSX_ARTIFACT_SIGNING_PROFILE` | `profile` |
| Signing tool | `--sign-tool` | `GOSX_ARTIFACT_SIGNING_TOOL` | `tool` |
| SignTool executable | `--signtool-path` | PATH lookup | `signtool_path` |
| Artifact Signing dlib | `--dlib-path` | `GOSX_ARTIFACT_SIGNING_DLIB` | `dlib_path` |
| Jsign executable | `--jsign-path` | PATH lookup | `jsign_path` |

Endpoint, account, and profile are required. The endpoint must be an HTTPS URL
without credentials, a query, or a fragment. `tool` is `signtool` or `jsign`;
it defaults to `signtool` on Windows and `jsign` elsewhere. SignTool also requires
a dlib path and a Windows host. Executables can be selected by path or found on
PATH. The provider checks its settings, tool availability, and jsign token before
bootstrapper downloads or installer builds.

For example, add this block to your existing package config:

```json
{
  "signing": {
    "endpoint": "https://signing.example.com",
    "account": "example-account",
    "profile": "example-profile",
    "tool": "jsign"
  }
}
```

Optional non-secret path keys are `dlib_path`, `signtool_path`, and `jsign_path`.
There are no token, tenant ID, client ID, or client secret keys in this block;
unknown keys are rejected. Signing settings are removed from the installed app's
config. `GOSX_ARTIFACT_SIGNING_TOKEN` is a secret environment variable for jsign,
with no flag or JSON equivalent. Inject it through your secret manager or CI.

## Windows: SignTool and the Azure dlib

Install Windows SDK SignTool and the Artifact Signing client prerequisites.
Microsoft currently documents the `Microsoft.ArtifactSigning.Client` NuGet
package and its `Azure.CodeSigning.Dlib.dll`; the previous package name was
`Microsoft.Trusted.Signing.Client`. Match the architectures of SignTool, dlib,
and the .NET runtime. The current integration guide also offers an installer
that bundles the required client tools.

The dlib authenticates using Azure `DefaultAzureCredential`. Options include
managed identity, an existing `az login`, or service principal environment
variables `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, and `AZURE_CLIENT_SECRET`.
See [Azure Identity for .NET](https://learn.microsoft.com/en-us/dotnet/api/overview/azure/identity-readme?view=azure-dotnet).
Keep those credentials in your secret manager and inject them into the process.

With Azure CLI authentication, run this in PowerShell:

```powershell
az login
$env:GOWORK = 'off'
$env:GOSX_ARTIFACT_SIGNING_ENDPOINT = 'https://signing.example.com'
$env:GOSX_ARTIFACT_SIGNING_ACCOUNT = 'example-account'
$env:GOSX_ARTIFACT_SIGNING_PROFILE = 'example-profile'
$env:GOSX_ARTIFACT_SIGNING_TOOL = 'signtool'
$env:GOSX_ARTIFACT_SIGNING_DLIB = 'C:\Signing\Azure.CodeSigning.Dlib.dll'

gosx desktop package --input .\stage --config .\package.json `
  --output .\dist\desktop --sign-provider azure-artifact-signing `
  --signtool-path 'C:\Signing\signtool.exe' `
  --verify-signatures --expect-subject 'Example Publisher'
```

GoSX writes a temporary `metadata.json` containing `Endpoint`,
`CodeSigningAccountName`, and `CertificateProfileName`. The directory is private,
the file is created with mode `0600`, and it is deleted after packaging. On
Windows, filesystem access is governed by the inherited Windows ACLs.
The per-file invocation uses an argument list:

```text
signtool sign /v /fd SHA256 /tr http://timestamp.acs.microsoft.com /td SHA256 /dlib <dlib> /dmdf <metadata.json> <file>
```

## Linux: jsign and an Azure access token

Install Java, jsign with `TRUSTEDSIGNING` support, and Azure CLI. For verification,
install osslsigncode with a trust store containing the required Microsoft roots,
or jsign with its documented `verify` command. The jsign project currently labels
`verify` as an 8.0 preview feature; a signing-only version does not suffice.

```sh
export GOWORK=off
az login
export GOSX_ARTIFACT_SIGNING_ENDPOINT='https://signing.example.com'
export GOSX_ARTIFACT_SIGNING_ACCOUNT='example-account'
export GOSX_ARTIFACT_SIGNING_PROFILE='example-profile'
export GOSX_ARTIFACT_SIGNING_TOOL='jsign'

gosx desktop package --input ./stage --config ./package.json \
  --output ./dist/desktop --sign-provider azure-artifact-signing \
  --verify-signatures --expect-subject 'Example Publisher'
```

If `GOSX_ARTIFACT_SIGNING_TOKEN` is absent, GoSX captures the token from this
command in memory:

```text
az account get-access-token --resource https://codesigning.azure.net --query accessToken --output tsv
```

Do not run the token command in a logged terminal or with shell tracing enabled.
The provider does not print the result or save the token to disk. It obtains one
token before packaging, so supply a fresh token for long builds.
The per-file jsign arguments are:

```text
jsign --storetype TRUSTEDSIGNING --keystore <endpoint> --alias <account>/<profile> --storepass env:GOSX_ARTIFACT_SIGNING_TOKEN --alg SHA-256 --tsaurl http://timestamp.acs.microsoft.com --tsmode RFC3161 <file>
```

The `env:` password reference keeps the token out of the argument list. GoSX
passes it in the child environment and redacts it from signing failure output.
The provider does not use shell command interpolation. Jsign retrieves a
certificate chain for each invocation; per-file signing may use more service
quota than a single jsign invocation with multiple files.

## Verify and inspect the package

```sh
gosx desktop verify-signature --expect-subject 'Example Publisher' \
  './dist/desktop/Example-App-Setup-1.0.0.exe' './portable/app.exe'
```

Packaging signs a temporary copy of `stage`. To verify the packaged application,
extract the portable ZIP and pass its PE files to `verify-signature`; the original
stage remains unchanged. To check the uninstaller, inspect the installed
`uninstall.exe`. `--verify-signatures` checks all staged PE copies, the embedded
uninstaller, and Setup automatically, immediately after each signing operation.
The separately supplied or downloaded Microsoft WebView2 bootstrapper retains
its existing signature and is not re-signed with your certificate profile.

On Windows, verification uses PowerShell `Get-AuthenticodeSignature` and requires
`Status` to be `Valid`. See [Microsoft's command reference](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.security/get-authenticodesignature).
Other hosts try `osslsigncode verify -in <file>` first, then
`jsign verify --verbose <file>`. An unavailable verifier produces a
`cannot verify on this host` error. `--jsign-path` selects the jsign fallback.
An invalid signature or signer mismatch fails the command.

`--expect-subject` uses a case-sensitive substring of the verified signer subject;
use a distinctive part of the publisher's CN. Package verification also requires
the same reported subject across all signed files. Jsign reports the leaf CN,
so its recorded subject has the form `CN=Example Publisher`. Windows and
osslsigncode can report a fuller distinguished name. Their trust and revocation
behavior differs: jsign documents that its verifier does not check CRL/OCSP
revocation. Verification must run on a host with the appropriate trust roots.

`package-metadata.json` records `signing_provider` (`azure-artifact-signing` or
`command`) and, when verification was requested, `verified_signer_subject`.
Without verification, the subject field is omitted. No credentials are recorded.

## CI and governed releases

The existing desktop CI gate runs `TestDesktopPackageAzureFakeTool` on a
GitHub-hosted Ubuntu runner (`go-tests` job). Its fake tool appends a test marker, and the test checks
signing order, payload contents, metadata, and final hashes without contacting
Azure. These markers are not Authenticode signatures.

Only `release-governed.yml` invokes real Azure signing in repository CI. The
Windows build matrix stays on GitHub-hosted `ubuntu-latest`. It signs in with
[Azure Login](https://github.com/Azure/login/blob/v3/README.md) using a service
principal and OpenID Connect (OIDC), then downloads pinned jsign 7.5, checks its
SHA-256, and puts a Java wrapper on PATH. Java is
[preinstalled on the Ubuntu runner](https://github.com/actions/runner-images/blob/main/images/ubuntu/Ubuntu2404-Readme.md#java).
The provisioning step also installs `osslsigncode` and `ca-certificates` for
verification: jsign 7.5 can sign but has no `verify` command.

Configure these values where the workflow's `build` job can read them:

| Name | GitHub storage | Purpose |
| --- | --- | --- |
| `AZURE_CLIENT_ID` | Secret | Service principal application client ID |
| `AZURE_TENANT_ID` | Secret | Microsoft Entra tenant ID |
| `AZURE_SUBSCRIPTION_ID` | Secret | Azure subscription ID |
| `GOSX_ARTIFACT_SIGNING_ENDPOINT` | Repository variable or secret | Regional HTTPS signing endpoint |
| `GOSX_ARTIFACT_SIGNING_ACCOUNT` | Repository variable or secret | Signing account name |
| `GOSX_ARTIFACT_SIGNING_PROFILE` | Repository variable or secret | Certificate profile name |

The workflow reads the three `AZURE_*` values from `secrets`. For each non-secret
`GOSX_ARTIFACT_SIGNING_*` setting it reads `secrets` first, then `vars` if the
secret is empty. The build job has no GitHub environment, so secrets scoped only
to the publish job's `governed-release` environment are unavailable here.

Create a federated credential on the service principal's Entra application:

- Issuer: `https://token.actions.githubusercontent.com`.
- Audience: `api://AzureADTokenExchange`.
- Subject for the current default-branch `workflow_dispatch`:
  `repo:<owner>/<repo>:ref:refs/heads/<default-branch>`.

Grant that identity the Artifact Signing Certificate Profile Signer role on the
profile. Only the `build` job receives `id-token: write`, alongside
`contents: read`. Azure Login establishes the Azure CLI session; GoSX then calls
`az account get-access-token --resource https://codesigning.azure.net` to obtain
a fresh signing token. The workflow does not read `GOSX_ARTIFACT_SIGNING_TOKEN`;
the CLI still supports it for local use.

The subject follows the workflow's trigger ref, not the release tag input or the
source checkout. This workflow accepts only default-branch dispatches, including
tag reruns. If policy is later changed to permit tag-triggered runs or dispatches
on a tag, their subject is `repo:<owner>/<repo>:ref:refs/tags/<tag>`; provision a
matching federated credential for each allowed tag. A tag subject does not
replace the default-branch subject for today's workflow. These examples assume
the standard name-based subject; if the repository uses customized or immutable
subjects, match its actual configured claim. See GitHub's
[OIDC subject reference](https://docs.github.com/en/actions/reference/security/oidc).

For a client-secret alternative, replace the OIDC login step's three `with`
inputs with `creds: ${{ secrets.AZURE_CREDENTIALS }}`. Store a JSON object in that
secret with `clientId`, `tenantId`, `subscriptionId`, and `clientSecret` fields,
using your service principal values. Keep the Windows and endpoint/account/profile
guards, remove the three `env.AZURE_*` guards, and remove `id-token: write` from
the build job when OIDC is no longer used. Do not combine `creds` with the three
OIDC inputs: Azure Login ignores `creds` when those inputs are set. Rotate the
client secret before its configured expiry. Both login methods let GoSX obtain
a fresh token through `az`; neither requires storing an hourly access token.

If endpoint, account, or profile is absent, the signing script logs a skip and
retains the existing unsigned release behavior; login and provisioning are skipped
too. If all three are configured, missing Azure credentials cause signing to fail
rather than publish an unsigned artifact. With authentication available, the step
packages and verifies a temporary smoke app, including its Setup and uninstaller,
and replaces the
published Windows CLI PE with the verified signed copy. The smoke installer is
removed and is not published or installed. A shipping desktop app must provide
its own package config and Ed25519 update key. No new runner jobs are added.

## Troubleshooting

- **Missing settings:** the error lists missing endpoint, account, profile, and
  dlib settings together. Check precedence: a flag overrides an environment
  value, which overrides JSON.
- **403 or signing failure:** confirm the endpoint region, profile, identity
  validation, and signer role. See Microsoft's
  [Artifact Signing FAQ](https://learn.microsoft.com/en-us/azure/artifact-signing/faq).
- **SignTool cannot load the dlib:** check SDK compatibility, the .NET runtime,
  Visual C++ runtime, dlib path, and architecture against Microsoft's guide.
- **No token:** run `az login` with the correct identity, or inject a fresh
  `GOSX_ARTIFACT_SIGNING_TOKEN`. GoSX does not fall back from an explicitly supplied
  token to a different identity.
- **Cannot verify on this host:** install a supported verifier. Older jsign
  releases can sign but lack `verify`.
- **Untrusted certificate:** update the verifier's trust store. An exit code
  indicating trust failure is not accepted as successful verification.
- **Subject mismatch:** inspect the certificate's actual publisher CN. The Azure
  account and profile names are not the certificate subject.
- **Timestamp failure:** allow access to Microsoft's documented
  `http://timestamp.acs.microsoft.com` service. Short-lived Artifact Signing
  certificates need a timestamp for validation after the signing certificate
  expires.

## What was verified

The Microsoft integration guide was checked for the current client package,
dlib metadata, authentication, SignTool arguments, and timestamp service.
The jsign project documentation was checked for `TRUSTEDSIGNING`, endpoint and
alias semantics, token acquisition, `env:` password references, RFC 3161
options, and the preview verifier's output and limitations. The
[osslsigncode project](https://github.com/mtrojnar/osslsigncode) supplies the
non-Windows verification tool; its
[verification implementation](https://github.com/mtrojnar/osslsigncode/blob/master/osslsigncode.c)
was checked to bind a reported leaf subject to a successful signature result.
The legacy NuGet package is listed at
[Microsoft.Trusted.Signing.Client](https://www.nuget.org/packages/Microsoft.Trusted.Signing.Client).

Local tests verify configuration precedence, validation, argument lists, token
handling, verification parsing, and complete packaging with a fake tool.
No live Azure account, signing identity, certificate profile, or access token was
used to validate a real signature. Windows SignTool/dlib execution, PowerShell
execution on Windows, actual jsign/osslsigncode certificate validation, and
release-runner provisioning were not exercised locally. Cross-platform vet
checks compilation; it does not establish those runtime results.
