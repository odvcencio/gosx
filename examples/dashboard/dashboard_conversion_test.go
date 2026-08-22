package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"m31labs.dev/gosx"
	_ "m31labs.dev/gosx/examples/dashboard/modules"
	"m31labs.dev/gosx/route"
)

// dashboardConversionGolden is the dashboard root page's rendered output,
// captured from app/page.gsx BEFORE gosx#248's conversion — when Page was
// `func Page() Node` reading `data.users`/`data.active`/`data.revenue`/
// `data.growth` from a Load hook returning map[string]string — with the
// footer's live clock read normalized to TIME and the framework
// version normalized to VERSION.
//
// This is the task's required proof: not that app/page.gsx compiles as
// `component Page(props: PageProps)`, but that it renders BYTE-IDENTICAL
// output once converted, reading the same Load hook's data through typed,
// proven props instead of the untyped `data` binding.
const dashboardConversionGolden = `<div class="layout"> <aside class="sidebar"> <h2>GoSX Dashboard</h2> <nav> <a href="/">Home</a> <a href="/users">Users</a> <a href="/users/new">New User</a> <a href="/counter">Counter</a> <a href="/kitchen-sink">Kitchen Sink</a> <a href="/settings">Settings</a> </nav> </aside> <main class="main">  <h1>Dashboard</h1> <div class="grid"> <div class="card"> <h3>Users</h3> <div class="stat">1,247</div> </div> <div class="card"> <h3>Active</h3> <div class="stat">892</div> </div> <div class="card"> <h3>Revenue</h3> <div class="stat">$48,290</div> </div> <div class="card"> <h3>Growth</h3> <div class="stat">+12.5%</div> </div> </div> <div class="card"> <h3>Recent Activity</h3> <table> <thead> <tr> <th>User</th> <th>Action</th> <th>When</th> </tr> </thead> <tbody> <tr> <td>Alice</td> <td>Created account</td> <td>2 min ago</td> </tr> <tr> <td>Bob</td> <td>Updated profile</td> <td>15 min ago</td> </tr> <tr> <td>Carol</td> <td>Uploaded document</td> <td>1 hour ago</td> </tr> <tr> <td>Dave</td> <td>Changed settings</td> <td>3 hours ago</td> </tr> <tr> <td>Eve</td> <td>Logged in</td> <td>5 hours ago</td> </tr> </tbody> </table> </div>  <div class="footer">GoSX VERSION — Server rendered at TIME</div> </main> </div>`

var dashboardFooterClockRE = regexp.MustCompile(`Server rendered at \d{2}:\d{2}:\d{2}`)

// dashboardFooterVersionRE normalizes the framework version the footer
// stamps. The golden proves the strict-props conversion renders identical
// BYTES; the running version is incidental to that claim, exactly like the
// clock above. Pinning it made every release fail this test until someone
// hand-edited the golden — it broke the v0.51.0 release.
var dashboardFooterVersionRE = regexp.MustCompile(`GoSX v\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?`)

// TestDashboardRootPageStrictPropsMatchLegacyBytes renders app/'s converted
// strict Page entry through the real Router.AddDir file-routing path (the
// same path main() builds) and asserts the response body, with the
// footer's live clock read normalized, is byte-identical to
// dashboardConversionGolden.
func TestDashboardRootPageStrictPropsMatchLegacyBytes(t *testing.T) {
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, content gosx.Node) gosx.Node {
		return content
	})
	if err := router.AddDir("app", route.FileRoutesOptions{}); err != nil {
		t.Fatalf("AddDir: %v", err)
	}
	handler, err := router.BuildChecked()
	if err != nil {
		t.Fatalf("BuildChecked: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	got := dashboardFooterClockRE.ReplaceAllString(rec.Body.String(), "Server rendered at TIME")
	got = dashboardFooterVersionRE.ReplaceAllString(got, "GoSX VERSION")
	if got != dashboardConversionGolden {
		t.Fatalf("rendered body does not byte-match the pre-conversion golden.\ngot:  %s\nwant: %s", got, dashboardConversionGolden)
	}
}
