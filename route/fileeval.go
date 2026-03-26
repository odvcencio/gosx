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
	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/server"
	"github.com/odvcencio/gosx/session"
)

type fileRenderEnv struct {
	values       map[string]any
	funcs        map[string]any
	components   map[string]any
	renderEngine func(engine.Config, gosx.Node) gosx.Node
}

type fileRequestBindings struct {
	requestPath   string
	method        string
	requestID     string
	query         map[string]string
	sessionValues map[string]any
	flashes       map[string][]any
	flash         map[string]any
	actions       map[string]any
	currentAction map[string]any
	user          any
	csrf          map[string]any
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
	next.renderEngine = env.renderEngine
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

func (env fileRenderEnv) engine(cfg engine.Config, fallback gosx.Node) gosx.Node {
	if env.renderEngine == nil {
		return fallback
	}
	return env.renderEngine(cfg, fallback)
}

func newFileRenderEnv(ctx *RouteContext, page FilePage) fileRenderEnv {
	bindings := buildFileRequestBindings(ctx)

	env := fileRenderEnv{
		values:     baseFileRenderValues(page, bindings),
		funcs:      map[string]any{},
		components: map[string]any{},
	}

	if ctx != nil {
		env.values["data"] = ctx.Data
		env.values["params"] = cloneStringMap(ctx.Params)
		env.renderEngine = ctx.Engine
		env.funcs["actionPath"] = func(name string) string {
			return ctx.ActionPath(name)
		}
	}
	registerBaseFileFuncs(&env, bindings)
	return env
}

func buildFileRequestBindings(ctx *RouteContext) fileRequestBindings {
	bindings := fileRequestBindings{
		query:         map[string]string{},
		sessionValues: map[string]any{},
		flashes:       map[string][]any{},
		flash:         map[string]any{},
		actions:       map[string]any{},
		currentAction: map[string]any{},
		csrf:          map[string]any{},
	}
	if ctx == nil || ctx.Request == nil {
		return bindings
	}

	bindings.requestPath = ctx.Request.URL.Path
	bindings.method = ctx.Request.Method
	bindings.requestID = serverRequestID(ctx.Request)
	bindings.query = flattenQueryValues(ctx.Request.URL.Query())
	bindings.sessionValues = session.Values(ctx.Request)
	bindings.flashes = session.FlashValues(ctx.Request)
	bindings.flash = firstFlashValues(bindings.flashes)
	bindings.actions = templateActionStates(action.States(ctx.Request))
	bindings.currentAction = preferredActionState(bindings.actions)
	if resolvedUser, ok := auth.Current(ctx.Request); ok {
		bindings.user = templateUser(resolvedUser)
	}
	if token := session.Token(ctx.Request); token != "" {
		bindings.csrf["token"] = token
		bindings.csrf["field"] = defaultCSRFFieldName()
	}
	return bindings
}

func baseFileRenderValues(page FilePage, bindings fileRequestBindings) map[string]any {
	return map[string]any{
		"data":    nil,
		"params":  map[string]string{},
		"query":   bindings.query,
		"session": bindings.sessionValues,
		"flash":   bindings.flash,
		"flashes": bindings.flashes,
		"actions": bindings.actions,
		"action":  bindings.currentAction,
		"csrf":    bindings.csrf,
		"user":    bindings.user,
		"page": map[string]any{
			"pattern": page.Pattern,
			"route":   page.RoutePath,
			"source":  page.Source,
			"path":    bindings.requestPath,
		},
		"request": map[string]any{
			"id":     bindings.requestID,
			"method": bindings.method,
			"path":   bindings.requestPath,
		},
	}
}

func registerBaseFileFuncs(env *fileRenderEnv, bindings fileRequestBindings) {
	env.funcs["len"] = fileEvalLen
	env.funcs["asset"] = server.AssetURL
	env.funcs["stylesheet"] = func(href string) gosx.Node {
		return server.Stylesheet(href)
	}
	env.funcs["flashValue"] = func(name string) any {
		return bindings.flash[name]
	}
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
	if value, ok := selectMappedValue(target, name); ok {
		return value
	}
	if value, ok := selectMethodValue(target, name); ok {
		return value
	}
	if value, ok := selectStructValue(target, name); ok {
		return value
	}
	return nil
}

func indexValue(target any, index any) any {
	if target == nil || index == nil {
		return nil
	}

	rv, ok := indirectValueOf(target)
	if !ok {
		return nil
	}

	switch rv.Kind() {
	case reflect.Map:
		return indexMapValue(rv, index)
	case reflect.Array, reflect.Slice, reflect.String:
		return indexSequentialValue(rv, index)
	default:
		return nil
	}
}

func selectMappedValue(target any, name string) (any, bool) {
	return mapLookup(target, name)
}

func selectMethodValue(target any, name string) (any, bool) {
	method, ok := methodValue(target, name)
	if !ok {
		return nil, false
	}
	return method.Interface(), true
}

func selectStructValue(target any, name string) (any, bool) {
	rv, ok := indirectValueOf(target)
	if !ok || rv.Kind() != reflect.Struct {
		return nil, false
	}
	field := rv.FieldByName(name)
	if !field.IsValid() || !field.CanInterface() {
		return nil, false
	}
	return field.Interface(), true
}

func indexMapValue(rv reflect.Value, index any) any {
	key, ok := reflectValue(index, rv.Type().Key())
	if !ok {
		return nil
	}
	value := rv.MapIndex(key)
	if !value.IsValid() || !value.CanInterface() {
		return nil
	}
	return value.Interface()
}

func indexSequentialValue(rv reflect.Value, index any) any {
	i := int(numericValue(index))
	if i < 0 || i >= rv.Len() {
		return nil
	}
	value := rv.Index(i)
	if !value.IsValid() || !value.CanInterface() {
		return nil
	}
	return value.Interface()
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

	callArgs, ok := buildCallArgs(rv.Type(), args)
	if !ok {
		return nil, false
	}
	results := rv.Call(callArgs)
	return unwrapCallResults(results)
}

func buildCallArgs(typ reflect.Type, args []any) ([]reflect.Value, bool) {
	if typ.IsVariadic() {
		return buildVariadicCallArgs(typ, args)
	}
	return buildFixedCallArgs(typ, args)
}

func buildFixedCallArgs(typ reflect.Type, args []any) ([]reflect.Value, bool) {
	if len(args) != typ.NumIn() {
		return nil, false
	}
	callArgs := make([]reflect.Value, 0, len(args))
	for i, arg := range args {
		value, ok := reflectValue(arg, typ.In(i))
		if !ok {
			return nil, false
		}
		callArgs = append(callArgs, value)
	}
	return callArgs, true
}

func buildVariadicCallArgs(typ reflect.Type, args []any) ([]reflect.Value, bool) {
	requiredArgs := typ.NumIn() - 1
	if len(args) < requiredArgs {
		return nil, false
	}
	callArgs, ok := buildFixedCallArgsPrefix(typ, args[:requiredArgs], requiredArgs)
	if !ok {
		return nil, false
	}
	variadicType := typ.In(typ.NumIn() - 1).Elem()
	for _, arg := range args[requiredArgs:] {
		value, ok := reflectValue(arg, variadicType)
		if !ok {
			return nil, false
		}
		callArgs = append(callArgs, value)
	}
	return callArgs, true
}

func buildFixedCallArgsPrefix(typ reflect.Type, args []any, count int) ([]reflect.Value, bool) {
	callArgs := make([]reflect.Value, 0, len(args))
	for i := 0; i < count; i++ {
		value, ok := reflectValue(args[i], typ.In(i))
		if !ok {
			return nil, false
		}
		callArgs = append(callArgs, value)
	}
	return callArgs, true
}

func unwrapCallResults(results []reflect.Value) (any, bool) {
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

	rv, ok := indirectValueOf(target)
	if !ok || rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
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
	if out, ok := reflectPointerTargetValue(value, target); ok {
		return out, true
	}
	rv := reflect.ValueOf(value)
	if out, ok := reflectDirectValue(rv, target); ok {
		return out, true
	}
	if out, ok := reflectPrimitiveValue(value, rv, target); ok {
		return out, true
	}
	return reflectStructuredValue(value, target)
}

func reflectPointerTargetValue(value any, target reflect.Type) (reflect.Value, bool) {
	if target.Kind() != reflect.Pointer {
		return reflect.Value{}, false
	}
	inner, ok := reflectValue(value, target.Elem())
	if !ok {
		return reflect.Value{}, false
	}
	out := reflect.New(target.Elem())
	out.Elem().Set(inner)
	return out, true
}

func reflectDirectValue(rv reflect.Value, target reflect.Type) (reflect.Value, bool) {
	if !rv.IsValid() {
		return reflect.Value{}, false
	}
	if rv.Type().AssignableTo(target) {
		return rv, true
	}
	if rv.Type().ConvertibleTo(target) {
		return rv.Convert(target), true
	}
	return reflect.Value{}, false
}

func reflectStructuredValue(value any, target reflect.Type) (reflect.Value, bool) {
	if target.Kind() == reflect.Struct {
		return reflectStructValue(value, target)
	}
	return reflect.Value{}, false
}

func reflectPrimitiveValue(value any, rv reflect.Value, target reflect.Type) (reflect.Value, bool) {
	switch target.Kind() {
	case reflect.String:
		return reflect.ValueOf(stringifyValue(value)).Convert(target), true
	case reflect.Bool:
		return reflect.ValueOf(truthy(value)).Convert(target), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflectSignedValue(value, target), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return reflectUnsignedValue(value, target), true
	case reflect.Float32, reflect.Float64:
		return reflectFloatValue(value, target), true
	case reflect.Interface:
		if rv.IsValid() {
			return rv, true
		}
	}
	return reflect.Value{}, false
}

func reflectSignedValue(value any, target reflect.Type) reflect.Value {
	out := reflect.New(target).Elem()
	out.SetInt(int64(numericValue(value)))
	return out
}

func reflectUnsignedValue(value any, target reflect.Type) reflect.Value {
	out := reflect.New(target).Elem()
	out.SetUint(uint64(numericValue(value)))
	return out
}

func reflectFloatValue(value any, target reflect.Type) reflect.Value {
	out := reflect.New(target).Elem()
	out.SetFloat(numericValue(value))
	return out
}

func indirectValueOf(value any) (reflect.Value, bool) {
	return indirectReflectValue(reflect.ValueOf(value))
}

func indirectReflectValue(rv reflect.Value) (reflect.Value, bool) {
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return reflect.Value{}, false
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return reflect.Value{}, false
	}
	return rv, true
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
	rv, ok := indirectReflectValue(rv)
	if !ok {
		return false
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

	rv, ok := indirectValueOf(value)
	if !ok {
		return 0
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
	rv, ok := indirectValueOf(value)
	return ok && rv.Kind() == reflect.String
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
	rv, ok := indirectValueOf(value)
	if !ok {
		return false
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
	rv, ok := indirectValueOf(value)
	if !ok {
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
