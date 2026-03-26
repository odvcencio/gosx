package route

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/action"
	"github.com/odvcencio/gosx/auth"
	"github.com/odvcencio/gosx/server"
	"github.com/odvcencio/gosx/session"
)

type fileRenderEnv struct {
	values     map[string]any
	funcs      map[string]any
	components map[string]any
}

func (env fileRenderEnv) clone() fileRenderEnv {
	next := fileRenderEnv{
		values:     make(map[string]any, len(env.values)),
		funcs:      make(map[string]any, len(env.funcs)),
		components: make(map[string]any, len(env.components)),
	}
	for key, value := range env.values {
		next.values[key] = value
	}
	for key, value := range env.funcs {
		next.funcs[key] = value
	}
	for key, value := range env.components {
		next.components[key] = value
	}
	return next
}

func (env fileRenderEnv) withValue(name string, value any) fileRenderEnv {
	next := env.clone()
	if next.values == nil {
		next.values = make(map[string]any)
	}
	next.values[name] = value
	return next
}

func (env fileRenderEnv) withBindings(bindings FileTemplateBindings) fileRenderEnv {
	next := env.clone()
	if next.values == nil {
		next.values = make(map[string]any)
	}
	if next.funcs == nil {
		next.funcs = make(map[string]any)
	}
	if next.components == nil {
		next.components = make(map[string]any)
	}
	for key, value := range bindings.Values {
		next.values[key] = value
	}
	for key, value := range bindings.Funcs {
		next.funcs[key] = value
	}
	for key, value := range bindings.Components {
		next.components[key] = value
	}
	return next
}

func (env fileRenderEnv) component(name string) (any, bool) {
	if env.components == nil {
		return nil, false
	}
	value, ok := env.components[name]
	return value, ok
}

func newFileRenderEnv(ctx *RouteContext, page FilePage) fileRenderEnv {
	requestPath := ""
	method := ""
	requestID := ""
	query := map[string]string{}
	sessionValues := map[string]any{}
	flashes := map[string][]any{}
	flash := map[string]any{}
	actions := map[string]any{}
	currentAction := map[string]any{}
	var user any
	csrf := map[string]any{}
	if ctx != nil && ctx.Request != nil {
		requestPath = ctx.Request.URL.Path
		method = ctx.Request.Method
		requestID = serverRequestID(ctx.Request)
		query = flattenQueryValues(ctx.Request.URL.Query())
		sessionValues = session.Values(ctx.Request)
		flashes = session.FlashValues(ctx.Request)
		flash = firstFlashValues(flashes)
		actions = templateActionStates(action.States(ctx.Request))
		currentAction = preferredActionState(actions)
		if resolvedUser, ok := auth.Current(ctx.Request); ok {
			user = templateUser(resolvedUser)
		}
		if token := session.Token(ctx.Request); token != "" {
			csrf["token"] = token
			csrf["field"] = defaultCSRFFieldName()
		}
	}

	env := fileRenderEnv{
		values: map[string]any{
			"data":    nil,
			"params":  map[string]string{},
			"query":   query,
			"session": sessionValues,
			"flash":   flash,
			"flashes": flashes,
			"actions": actions,
			"action":  currentAction,
			"csrf":    csrf,
			"user":    user,
			"page": map[string]any{
				"pattern": page.Pattern,
				"route":   page.RoutePath,
				"source":  page.Source,
				"path":    requestPath,
			},
			"request": map[string]any{
				"id":     requestID,
				"method": method,
				"path":   requestPath,
			},
		},
		funcs:      map[string]any{},
		components: map[string]any{},
	}

	if ctx != nil {
		env.values["data"] = ctx.Data
		env.values["params"] = cloneStringMap(ctx.Params)
		env.funcs["actionPath"] = func(name string) string {
			return ctx.ActionPath(name)
		}
	}
	env.funcs["len"] = fileEvalLen
	env.funcs["asset"] = server.AssetURL
	env.funcs["stylesheet"] = func(href string) gosx.Node {
		return server.Stylesheet(href)
	}
	env.funcs["flashValue"] = func(name string) any {
		return flash[name]
	}
	return env
}

func evalStaticFileExpr(expr string) any {
	return evalFileExpr(expr, fileRenderEnv{})
}

func evalFileExpr(expr string, env fileRenderEnv) any {
	parsed, err := parser.ParseExpr(strings.TrimSpace(expr))
	if err != nil {
		return nil
	}
	return evalFileNode(parsed, env)
}

func evalFileNode(expr ast.Expr, env fileRenderEnv) any {
	switch node := expr.(type) {
	case *ast.BasicLit:
		return evalBasicLit(node)
	case *ast.Ident:
		return evalIdent(node.Name, env)
	case *ast.BinaryExpr:
		return evalBinaryExpr(node, env)
	case *ast.UnaryExpr:
		return evalUnaryExpr(node, env)
	case *ast.ParenExpr:
		return evalFileNode(node.X, env)
	case *ast.SelectorExpr:
		return selectValue(evalFileNode(node.X, env), node.Sel.Name)
	case *ast.IndexExpr:
		return indexValue(evalFileNode(node.X, env), evalFileNode(node.Index, env))
	case *ast.CallExpr:
		return callValue(evalFileNode(node.Fun, env), evalCallArgs(node.Args, env))
	default:
		return nil
	}
}

func evalBasicLit(node *ast.BasicLit) any {
	switch node.Kind {
	case token.STRING, token.CHAR:
		value, err := strconvUnquote(node.Value)
		if err != nil {
			return nil
		}
		return value
	case token.INT:
		return parseInt(node.Value)
	case token.FLOAT:
		return parseFloat(node.Value)
	default:
		return nil
	}
}

func evalIdent(name string, env fileRenderEnv) any {
	switch name {
	case "true":
		return true
	case "false":
		return false
	case "nil":
		return nil
	}
	if env.values != nil {
		if value, ok := env.values[name]; ok {
			return value
		}
	}
	if env.funcs != nil {
		if fn, ok := env.funcs[name]; ok {
			return fn
		}
	}
	return nil
}

func evalBinaryExpr(node *ast.BinaryExpr, env fileRenderEnv) any {
	left := evalFileNode(node.X, env)
	right := evalFileNode(node.Y, env)

	switch node.Op {
	case token.ADD:
		if isStringLike(left) || isStringLike(right) {
			return stringifyValue(left) + stringifyValue(right)
		}
		return numericValue(left) + numericValue(right)
	case token.SUB:
		return numericValue(left) - numericValue(right)
	case token.MUL:
		return numericValue(left) * numericValue(right)
	case token.QUO:
		divisor := numericValue(right)
		if divisor == 0 {
			return 0
		}
		return numericValue(left) / divisor
	case token.REM:
		divisor := int64(numericValue(right))
		if divisor == 0 {
			return 0
		}
		return int64(numericValue(left)) % divisor
	case token.LAND:
		return truthy(left) && truthy(right)
	case token.LOR:
		return truthy(left) || truthy(right)
	case token.EQL:
		return equalValues(left, right)
	case token.NEQ:
		return !equalValues(left, right)
	case token.GTR:
		return compareValues(left, right) > 0
	case token.GEQ:
		return compareValues(left, right) >= 0
	case token.LSS:
		return compareValues(left, right) < 0
	case token.LEQ:
		return compareValues(left, right) <= 0
	default:
		return nil
	}
}

func evalUnaryExpr(node *ast.UnaryExpr, env fileRenderEnv) any {
	value := evalFileNode(node.X, env)
	switch node.Op {
	case token.NOT:
		return !truthy(value)
	case token.SUB:
		return -numericValue(value)
	case token.ADD:
		return numericValue(value)
	default:
		return nil
	}
}

func evalCallArgs(args []ast.Expr, env fileRenderEnv) []any {
	values := make([]any, 0, len(args))
	for _, arg := range args {
		values = append(values, evalFileNode(arg, env))
	}
	return values
}

func selectValue(target any, name string) any {
	if target == nil {
		return nil
	}

	if value, ok := mapLookup(target, name); ok {
		return value
	}

	if method, ok := methodValue(target, name); ok {
		return method.Interface()
	}

	rv := reflect.ValueOf(target)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil
	}
	if rv.Kind() == reflect.Struct {
		field := rv.FieldByName(name)
		if field.IsValid() && field.CanInterface() {
			return field.Interface()
		}
	}

	return nil
}

func indexValue(target any, index any) any {
	if target == nil || index == nil {
		return nil
	}

	rv := reflect.ValueOf(target)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil
	}

	switch rv.Kind() {
	case reflect.Map:
		key, ok := reflectValue(index, rv.Type().Key())
		if !ok {
			return nil
		}
		value := rv.MapIndex(key)
		if !value.IsValid() || !value.CanInterface() {
			return nil
		}
		return value.Interface()
	case reflect.Array, reflect.Slice, reflect.String:
		i := int(numericValue(index))
		if i < 0 || i >= rv.Len() {
			return nil
		}
		value := rv.Index(i)
		if !value.IsValid() || !value.CanInterface() {
			return nil
		}
		return value.Interface()
	default:
		return nil
	}
}

func callValue(fn any, args []any) any {
	value, _ := tryCallValue(fn, args)
	return value
}

func tryCallValue(fn any, args []any) (any, bool) {
	if fn == nil {
		return nil, false
	}

	rv := reflect.ValueOf(fn)
	if !rv.IsValid() || rv.Kind() != reflect.Func {
		return nil, false
	}

	typ := rv.Type()
	callArgs := make([]reflect.Value, 0, len(args))
	if typ.IsVariadic() {
		if len(args) < typ.NumIn()-1 {
			return nil, false
		}
		for i := 0; i < typ.NumIn()-1; i++ {
			value, ok := reflectValue(args[i], typ.In(i))
			if !ok {
				return nil, false
			}
			callArgs = append(callArgs, value)
		}
		variadicType := typ.In(typ.NumIn() - 1).Elem()
		for i := typ.NumIn() - 1; i < len(args); i++ {
			value, ok := reflectValue(args[i], variadicType)
			if !ok {
				return nil, false
			}
			callArgs = append(callArgs, value)
		}
	} else {
		if len(args) != typ.NumIn() {
			return nil, false
		}
		for i := 0; i < typ.NumIn(); i++ {
			value, ok := reflectValue(args[i], typ.In(i))
			if !ok {
				return nil, false
			}
			callArgs = append(callArgs, value)
		}
	}

	results := rv.Call(callArgs)
	switch len(results) {
	case 0:
		return nil, true
	case 1:
		if results[0].CanInterface() {
			return results[0].Interface(), true
		}
		return nil, true
	case 2:
		if err, ok := results[1].Interface().(error); ok && err != nil {
			return nil, false
		}
		if results[0].CanInterface() {
			return results[0].Interface(), true
		}
		return nil, true
	default:
		values := make([]any, 0, len(results))
		for _, result := range results {
			if result.CanInterface() {
				values = append(values, result.Interface())
			}
		}
		return values, true
	}
}

func methodValue(target any, name string) (reflect.Value, bool) {
	rv := reflect.ValueOf(target)
	if !rv.IsValid() {
		return reflect.Value{}, false
	}
	method := rv.MethodByName(name)
	if method.IsValid() {
		return method, true
	}
	if rv.Kind() != reflect.Pointer && rv.CanAddr() {
		method = rv.Addr().MethodByName(name)
		if method.IsValid() {
			return method, true
		}
	}
	return reflect.Value{}, false
}

func mapLookup(target any, key string) (any, bool) {
	switch m := target.(type) {
	case map[string]any:
		value, ok := m[key]
		return value, ok
	case map[string]string:
		value, ok := m[key]
		return value, ok
	case map[string]int:
		value, ok := m[key]
		return value, ok
	case map[string]bool:
		value, ok := m[key]
		return value, ok
	}

	rv := reflect.ValueOf(target)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, false
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil, false
	}
	value := rv.MapIndex(reflect.ValueOf(key).Convert(rv.Type().Key()))
	if !value.IsValid() || !value.CanInterface() {
		return nil, false
	}
	return value.Interface(), true
}

func reflectValue(value any, target reflect.Type) (reflect.Value, bool) {
	if target == nil {
		return reflect.Value{}, false
	}
	if value == nil {
		return reflect.Zero(target), true
	}
	if target.Kind() == reflect.Pointer {
		inner, ok := reflectValue(value, target.Elem())
		if !ok {
			return reflect.Value{}, false
		}
		out := reflect.New(target.Elem())
		out.Elem().Set(inner)
		return out, true
	}
	rv := reflect.ValueOf(value)
	if rv.Type().AssignableTo(target) {
		return rv, true
	}
	if rv.Type().ConvertibleTo(target) {
		return rv.Convert(target), true
	}

	switch target.Kind() {
	case reflect.String:
		return reflect.ValueOf(stringifyValue(value)).Convert(target), true
	case reflect.Bool:
		return reflect.ValueOf(truthy(value)).Convert(target), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		out := reflect.New(target).Elem()
		out.SetInt(int64(numericValue(value)))
		return out, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		out := reflect.New(target).Elem()
		out.SetUint(uint64(numericValue(value)))
		return out, true
	case reflect.Float32, reflect.Float64:
		out := reflect.New(target).Elem()
		out.SetFloat(numericValue(value))
		return out, true
	case reflect.Struct:
		return reflectStructValue(value, target)
	case reflect.Interface:
		if rv.IsValid() {
			return rv, true
		}
	}
	return reflect.Value{}, false
}

func reflectStructValue(value any, target reflect.Type) (reflect.Value, bool) {
	props := spreadProps(value)
	if len(props) == 0 {
		return reflect.Value{}, false
	}

	out := reflect.New(target).Elem()
	assigned := false
	for i := 0; i < target.NumField(); i++ {
		field := target.Field(i)
		if !field.IsExported() {
			continue
		}
		fieldValue, ok := lookupTemplatePropValue(props, field.Name)
		if !ok {
			continue
		}
		converted, ok := reflectValue(fieldValue, field.Type)
		if !ok {
			continue
		}
		out.Field(i).Set(converted)
		assigned = true
	}
	return out, assigned
}

func lookupTemplatePropValue(props map[string]any, name string) (any, bool) {
	if props == nil {
		return nil, false
	}
	for _, candidate := range []string{name, exportedPropAlias(name), unexportedPropAlias(name), strings.ToLower(name)} {
		if candidate == "" {
			continue
		}
		if value, ok := props[candidate]; ok {
			return value, true
		}
	}
	return nil, false
}

func truthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != "" && v != "0" && v != "false"
	case gosx.Node:
		return !v.IsZero()
	}

	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return false
	}
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return false
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Bool:
		return rv.Bool()
	case reflect.String:
		return rv.Len() > 0
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint() != 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() != 0
	case reflect.Slice, reflect.Array, reflect.Map:
		return rv.Len() > 0
	case reflect.Struct:
		return true
	default:
		return !rv.IsZero()
	}
}

func numericValue(value any) float64 {
	switch v := value.(type) {
	case nil:
		return 0
	case int:
		return float64(v)
	case int8:
		return float64(v)
	case int16:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case uint:
		return float64(v)
	case uint8:
		return float64(v)
	case uint16:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	case float32:
		return float64(v)
	case float64:
		return v
	case bool:
		if v {
			return 1
		}
		return 0
	case string:
		if n, ok := parseFloatOK(strings.TrimSpace(v)); ok {
			return n
		}
		return 0
	}

	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return 0
	}
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return 0
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint())
	case reflect.Float32, reflect.Float64:
		return rv.Float()
	case reflect.Bool:
		if rv.Bool() {
			return 1
		}
	}
	return 0
}

func stringifyValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case gosx.Node:
		return gosx.RenderHTML(v)
	default:
		return fmt.Sprint(v)
	}
}

func isStringLike(value any) bool {
	if value == nil {
		return false
	}
	switch value.(type) {
	case string, fmt.Stringer:
		return true
	}
	rv := reflect.ValueOf(value)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return false
		}
		rv = rv.Elem()
	}
	return rv.IsValid() && rv.Kind() == reflect.String
}

func equalValues(left, right any) bool {
	if left == nil || right == nil {
		return left == right
	}
	if isNumeric(left) || isNumeric(right) {
		return numericValue(left) == numericValue(right)
	}
	return reflect.DeepEqual(left, right)
}

func compareValues(left, right any) int {
	if isStringLike(left) || isStringLike(right) {
		return strings.Compare(stringifyValue(left), stringifyValue(right))
	}
	ln := numericValue(left)
	rn := numericValue(right)
	switch {
	case ln < rn:
		return -1
	case ln > rn:
		return 1
	default:
		return 0
	}
}

func isNumeric(value any) bool {
	switch value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	}
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return false
	}
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return false
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func flattenQueryValues(values url.Values) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	flat := make(map[string]string, len(values))
	for key, entries := range values {
		if len(entries) > 0 {
			flat[key] = entries[0]
		}
	}
	return flat
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func fileEvalLen(value any) int {
	rv := reflect.ValueOf(value)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return 0
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return 0
	}
	switch rv.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		return rv.Len()
	default:
		return 0
	}
}

func serverRequestID(r *http.Request) string {
	if r == nil {
		return ""
	}
	return r.Header.Get("X-Request-ID")
}

func firstFlashValues(values map[string][]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(values))
	for key, entries := range values {
		if len(entries) > 0 {
			out[key] = entries[0]
		}
	}
	return out
}

func preferredActionState(states map[string]any) map[string]any {
	if len(states) == 1 {
		for _, state := range states {
			if value, ok := state.(map[string]any); ok {
				return value
			}
		}
	}
	return map[string]any{}
}

func defaultCSRFFieldName() string {
	return "csrf_token"
}

func templateActionStates(states map[string]action.View) map[string]any {
	if len(states) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(states))
	for name, view := range states {
		out[name] = templateActionState(view)
	}
	return out
}

func templateActionState(view action.View) map[string]any {
	return map[string]any{
		"name":        view.Name,
		"status":      view.Status,
		"ok":          view.Result.OK,
		"message":     view.Result.Message,
		"redirect":    view.Result.Redirect,
		"values":      cloneStringMap(view.Result.Values),
		"fieldErrors": cloneStringMap(view.Result.FieldErrors),
	}
}

func templateUser(user auth.User) map[string]any {
	return map[string]any{
		"id":    user.ID,
		"email": user.Email,
		"name":  user.Name,
		"roles": append([]string(nil), user.Roles...),
		"meta":  cloneAnyMap(user.Meta),
	}
}

func cloneAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func strconvUnquote(value string) (string, error) {
	return strconv.Unquote(value)
}

func parseInt(value string) int64 {
	n, _ := strconv.ParseInt(value, 10, 64)
	return n
}

func parseFloat(value string) float64 {
	n, _ := strconv.ParseFloat(value, 64)
	return n
}

func parseFloatOK(value string) (float64, bool) {
	n, err := strconv.ParseFloat(value, 64)
	return n, err == nil
}
