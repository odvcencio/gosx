package desktop

import (
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// StartupData contains primitive startup values consumed by typed browser APIs.
// It cannot supply executable code. Existing application keys can be retained.
type StartupData struct {
	Strings map[string]string
	Flags   map[string]bool
}

func scriptJSON(value any) string { data, _ := json.Marshal(value); return string(data) }
func scriptProperty(base, key string) string {
	valid := key != ""
	for i, c := range key {
		if !(c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9') {
			valid = false
			break
		}
	}
	if valid {
		return base + "." + key
	}
	return base + "[" + scriptJSON(key) + "]"
}

// StartupDataBootstrap safely encodes startup values, including HTML delimiters
// and Unicode line separators. It contains no application-supplied executable JS.
func StartupDataBootstrap(data StartupData) string {
	keys := make([]string, 0, len(data.Strings)+len(data.Flags))
	for k := range data.Strings {
		keys = append(keys, k)
	}
	for k := range data.Flags {
		if _, exists := data.Strings[k]; !exists {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out strings.Builder
	for _, key := range keys {
		out.WriteString(scriptProperty("window", key))
		out.WriteByte('=')
		if v, ok := data.Strings[key]; ok {
			out.WriteString(scriptJSON(v))
		} else {
			out.WriteString(strconv.FormatBool(data.Flags[key]))
		}
		out.WriteString(";\n")
	}
	return out.String()
}

type HostTelemetryConfig struct {
	Endpoint string `json:"endpoint"`
	Enabled  bool   `json:"enabled"`
}
type DocumentMarker struct {
	Name  string
	Value string
}

// HostFailurePage supplies presentation only. HTML must be trusted application
// markup without scripts or inline handlers; errors and logs use textContent.
type HostFailurePage struct {
	HTML         string         `json:"html"`
	MessageID    string         `json:"messageID"`
	DetailsID    string         `json:"detailsID"`
	RetryID      string         `json:"retryID"`
	StatusID     string         `json:"statusID"`
	Marker       DocumentMarker `json:"marker"`
	DefaultError string         `json:"defaultError"`
	EmptyDetails string         `json:"emptyDetails"`
	StartingText string         `json:"startingText"`
	RetryError   string         `json:"retryError"`
}

// HostProcessFailureTest is an explicit test hook. It sends only from the
// configured origin and records its sent flag before sending, once per document.
type HostProcessFailureTest struct {
	Kind        ProcessFailedKind
	MessageType string
	SentFlag    string
}

// HostLifecycleOptions adapts a native host service exposing status(), retry(),
// and launch(). Status returns {state,error,details,navigate}; launch performs
// native navigation. Application policy and message names stay in these options.
type HostLifecycleOptions struct {
	Origin                string
	ReadyPath             string
	ReadyMessageType      string
	ReportDOMReady        bool
	AllowBlankLoadingPage bool
	ServiceName           string
	PollInterval          time.Duration
	Startup               StartupData
	DocumentMarker        DocumentMarker
	Telemetry             *HostTelemetryConfig
	FailurePage           HostFailurePage
	ProcessFailureTest    *HostProcessFailureTest
}

// HostLifecycleBootstrap creates the native startup/failure watchdog. Polling is
// bounded to one pending request. Unload cancels timers/listeners and suppresses
// late completions. Only the exact origin, or explicitly allowed top-level blank
// loading document, can invoke the host service; messages require the origin.
func HostLifecycleBootstrap(o HostLifecycleOptions) (string, error) {
	u, err := url.Parse(o.Origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("desktop: invalid host lifecycle origin")
	}
	if o.ServiceName == "" {
		o.ServiceName = "host"
	}
	if o.ReadyPath == "" {
		o.ReadyPath = "/"
	}
	if o.PollInterval == 0 {
		o.PollInterval = 500 * time.Millisecond
	}
	if o.PollInterval < time.Millisecond {
		return "", errors.New("desktop: invalid host poll interval")
	}
	if o.ReportDOMReady && o.ReadyMessageType == "" {
		return "", errors.New("desktop: missing readiness message type")
	}
	if t := o.ProcessFailureTest; t != nil && (t.MessageType == "" || t.SentFlag == "" || t.Kind != ProcessFailedRenderProcessExited && t.Kind != ProcessFailedGPUProcessExited) {
		return "", errors.New("desktop: invalid process failure test")
	}
	config := struct {
		AllowBlank bool            `json:"allowBlank"`
		Path       string          `json:"path"`
		Report     bool            `json:"report"`
		Interval   int64           `json:"interval"`
		Page       HostFailurePage `json:"page"`
	}{o.AllowBlankLoadingPage, o.ReadyPath, o.ReportDOMReady, o.PollInterval.Milliseconds(), o.FailurePage}
	replacements := []string{
		"@ORIGIN@", scriptJSON(o.Origin), "@CONFIG@", scriptJSON(config), "@SERVICE@", scriptJSON(o.ServiceName), "@READY@", scriptJSON(o.ReadyMessageType), "@STARTUP@", StartupDataBootstrap(o.Startup),
		"@MARKER@", "", "@TELEMETRY@", "", "@TEST@", "",
	}
	if o.DocumentMarker.Name != "" {
		replacements[11] = "if(document.documentElement)" + scriptProperty("document.documentElement.dataset", o.DocumentMarker.Name) + "=" + scriptJSON(o.DocumentMarker.Value) + ";"
	}
	if o.Telemetry != nil {
		replacements[13] = "window.__gosx_telemetry_config=Object.assign({},window.__gosx_telemetry_config||{}," + scriptJSON(o.Telemetry) + ");"
	}
	if t := o.ProcessFailureTest; t != nil {
		flag := scriptProperty("window", t.SentFlag)
		replacements[15] = "if(sameOrigin()&&!" + flag + "){" + flag + "=true;post({type:" + scriptJSON(t.MessageType) + ",kind:" + scriptJSON(t.Kind) + "});}"
	}
	return strings.NewReplacer(replacements...).Replace(hostLifecycleScript), nil
}

const hostLifecycleScript = `(function(){
const origin=@ORIGIN@,options=@CONFIG@;
let disposed=false,started=false,pending=false,retrying=false,launching=false,timer=0,startTimer=0,button=null,status=null,retryQueued=false;
function sameOrigin(){return !disposed&&window.top===window&&location.origin===origin;}
function allowed(){return sameOrigin()||(!disposed&&options.allowBlank&&window.top===window&&location.href==="about:blank");}
function bridge(){return allowed()&&window.gosxDesktop&&window.gosxDesktop.__gosxDesktopBridge===true&&typeof window.gosxDesktop.service==="function";}
if(!allowed())return;
@TELEMETRY@
@STARTUP@
function post(message){if(!sameOrigin())return;try{window.chrome.webview.postMessage(JSON.stringify(message));}catch(_){} }
function reportReady(){if(sameOrigin()&&location.pathname===options.path)post({type:@READY@,href:location.href});}
function reportTestProcessFailure(){@TEST@}
function retry(){
 if(!bridge()||retrying||launching)return;
 if(pending){retryQueued=true;return;}
 retrying=true;if(button)button.disabled=true;if(status)status.textContent=options.page.startingText;
 let operation;try{operation=window.gosxDesktop.service(@SERVICE@).retry();}catch(error){failed(error);return;}
 Promise.resolve(operation).then(function(){retrying=false;if(allowed())poll();},failed);
 function failed(error){retrying=false;if(!allowed())return;if(button)button.disabled=false;if(status)status.textContent=error&&error.message?error.message:options.page.retryError;}
}
function showFailure(state){
 if(!allowed()||!document.documentElement||document.documentElement.dataset[options.page.marker.Name]===options.page.marker.Value)return;
 document.documentElement.innerHTML=options.page.html;
 document.documentElement.dataset[options.page.marker.Name]=options.page.marker.Value;
 function text(id,value){const node=document.getElementById(id);if(node)node.textContent=value;}
 text(options.page.messageID,state.error||options.page.defaultError);
 text(options.page.detailsID,(state.details||[]).join("\n")||options.page.emptyDetails);
 button=document.getElementById(options.page.retryID);status=document.getElementById(options.page.statusID);
 if(button)button.addEventListener("click",retry);
}
function poll(){
 if(!bridge()||pending||retrying||launching)return;
 pending=true;let operation;try{operation=window.gosxDesktop.service(@SERVICE@).status();}catch(_){finish();return;}
 Promise.resolve(operation).then(function(state){
  if(!allowed())return;
  if(state.navigate){
   launching=true;let navigation;try{navigation=window.gosxDesktop.service(@SERVICE@).launch();}catch(_){launching=false;showFailure(state);return;}
   Promise.resolve(navigation).then(function(){launching=false;},function(){launching=false;if(allowed())showFailure(state);});
  }else if(state.state==="failed")showFailure(state);
 }).catch(function(){}).then(finish,finish);
 function finish(){pending=false;if(retryQueued&&allowed()){retryQueued=false;retry();}}
}
function dispose(){if(disposed)return;disposed=true;clearInterval(timer);clearTimeout(startTimer);document.removeEventListener("DOMContentLoaded",start);window.removeEventListener("pagehide",dispose);window.removeEventListener("unload",dispose);if(button)button.removeEventListener("click",retry);}
function start(){if(started||!allowed())return;started=true;@MARKER@if(options.report)reportReady();reportTestProcessFailure();poll();timer=setInterval(poll,options.interval);}
window.addEventListener("pagehide",dispose);window.addEventListener("unload",dispose);
if(document.readyState==="loading")document.addEventListener("DOMContentLoaded",start,{once:true});else startTimer=setTimeout(start,0);
})();`
