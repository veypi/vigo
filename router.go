// router.go
// Copyright (C) 2024 veypi <i@veypi.com>
// 2024-08-07 13:45
// Distributed under terms of the MIT license.
package vigo

import (
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/veypi/vigo/logv"
)

var allowedMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
	http.MethodPatch, http.MethodDelete, http.MethodConnect,
	http.MethodOptions, http.MethodTrace, "PROPFIND", "ANY",
}

func NewRouter() Router {
	r := &route{
		kind:              nodeStatic,
		fragment:          "",
		funcBefore:        make([]any, 0, 10),
		funcBeforeInfo:    make([]*HandlerInfo, 0, 10),
		funcAfter:         make([]any, 0, 10),
		funcAfterInfo:     make([]*HandlerInfo, 0, 10),
		methods:           make(map[string]*RouteHandler),
		children:          make([]*route, 0),
		handlersInfoCache: make(map[string][]*HandlerInfo),
	}
	return r
}

type Router interface {
	GetParamsList() []string
	ServeHTTP(http.ResponseWriter, *http.Request)
	SubRouter(prefix string) Router
	String() string

	// Doc returns the API documentation structure
	Doc() *Doc

	Clear(url string, method string)
	Set(url string, method string, handlers ...any) Router
	Get(url string, handlers ...any) Router
	Any(url string, handlers ...any) Router
	Post(url string, handlers ...any) Router
	Head(url string, handlers ...any) Router
	Put(url string, handlers ...any) Router
	Patch(url string, handlers ...any) Router
	Delete(url string, handlers ...any) Router

	Use(middleware ...any) Router
	After(middleware ...any) Router
	Replace(Router) Router
	Extend(string, Router) Router
}

type nodeType int

const (
	nodeStatic   nodeType = iota // /a
	nodeParam                    // {param}
	nodeWildcard                 // * or {path:*}
	nodeCatchAll                 // **
	nodeRegex                    // {name:[a-z]+} or {a}.{b}
)

type route struct {
	kind     nodeType
	fragment string // raw fragment

	// For Regex/Composite nodes
	regex     *regexp.Regexp
	paramKeys []string // names of params in regex

	// For simple Param/Wildcard
	paramName string

	children []*route
	parent   *route

	// Handlers
	funcBefore        []any
	funcBeforeInfo    []*HandlerInfo
	funcAfter         []any
	funcAfterInfo     []*HandlerInfo
	handlersCache     map[string][]any
	handlersInfoCache map[string][]*HandlerInfo

	methods map[string]*RouteHandler

	config *Config
}

type HandlerInfo struct {
	Func   any
	Name   string
	File   string
	Line   int
	Scoped string
}

func getFuncName(f any) string {
	val := reflect.ValueOf(f)
	if val.Kind() == reflect.Func {
		return runtime.FuncForPC(val.Pointer()).Name()
	}
	return ""
}

type RouteHandler struct {
	Handlers     []any
	HandlersInfo []*HandlerInfo
	Caller       [3]string
	Desc         string
	Args         any
	Response     any
	ArgsDesc     string
}

// String() => /router/path
func (r *route) String() string {
	if r.parent != nil {
		return r.parent.String() + "/" + r.fragment
	}
	return r.fragment
}

func (r *route) GetParamsList() []string {
	var res []string
	tr := r
	for tr != nil {
		if tr.kind != nodeStatic {
			res = append(res, tr.fragment)
		}
		tr = tr.parent
	}
	slices.Reverse(res)
	return res
}

// match tries to match the path segments against children.
// path: full path string
// start: start index of current segment
// x: context to set params
// Returns: matched route, handlers, handler infos
func (r *route) match(path string, start int, method string, x *X) (*route, []any, []*HandlerInfo) {
	// Base case: No more segments
	if start >= len(path) {
		if r.methods[method] != nil {
			return r, r.handlersCache[method], r.handlersInfoCache[method]
		} else if r.methods["ANY"] != nil {
			return r, r.handlersCache["ANY"], r.handlersInfoCache["ANY"]
		}
		// Try to find a child that matches empty? (e.g. optional params? not supported yet, or CatchAll)
		for _, child := range r.children {
			if child.kind == nodeCatchAll {
				// ** matches empty? usually yes
				stackLen := len(x.PathParams)
				if child.paramName != "" {
					x.PathParams = append(x.PathParams, Param{child.paramName, ""})
				}
				res, h, info := child.match(path, start, method, x)
				if res != nil {
					return res, h, info
				}
				x.PathParams = x.PathParams[:stackLen]
			}
		}
		return nil, nil, nil
	}

	// Find end of current segment
	end := strings.IndexByte(path[start:], '/')
	if end == -1 {
		end = len(path)
	} else {
		end += start
	}
	seg := path[start:end]

	for _, child := range r.children {
		matched := false
		var nextStart int
		stackLen := len(x.PathParams)

		switch child.kind {
		case nodeStatic:
			if child.fragment == seg {
				matched = true
				nextStart = end + 1
			}
		case nodeParam:
			matched = true
			nextStart = end + 1
			if child.paramName != "" {
				x.PathParams = append(x.PathParams, Param{child.paramName, seg})
			}
		case nodeWildcard:
			// * or {path:*} matches one segment
			matched = true
			nextStart = end + 1
			if child.paramName != "" {
				x.PathParams = append(x.PathParams, Param{child.paramName, seg})
			}
		case nodeCatchAll:
			// ** matches all remaining segments
			matched = true
			nextStart = len(path)
			if child.paramName != "" {
				x.PathParams = append(x.PathParams, Param{child.paramName, path[start:]})
			}
		case nodeRegex:
			if child.regex != nil {
				subs := child.regex.FindStringSubmatch(seg)
				if subs != nil && subs[0] == seg { // Full match
					matched = true
					nextStart = end + 1
					for i, name := range child.regex.SubexpNames() {
						if i > 0 && i < len(subs) && name != "" {
							x.PathParams = append(x.PathParams, Param{name, subs[i]})
						}
					}
				}
			}
		}

		if matched {
			if nextStart > len(path) {
				nextStart = len(path)
			}
			// Recurse
			res, h, info := child.match(path, nextStart, method, x)

			if res != nil {
				return res, h, info
			}

			// Backtrack: reset params stack
			x.PathParams = x.PathParams[:stackLen]
		}
	}
	return nil, nil, nil
}

func (r *route) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	x := acquire()
	defer release(x)
	x.Request = req
	x.writer = w

	path := req.URL.Path
	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}
	if len(path) > 0 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}

	if subR, fcs, infos := r.match(path, 0, req.Method, x); subR != nil && len(fcs) > 0 {
		skipIdx := -1
		for i := range fcs {
			if _, ok := fcs[i].(FuncSkipBefore); ok {
				skipIdx = i
			}
		}
		if skipIdx >= 0 {
			fcs = fcs[skipIdx+1:]
			infos = infos[skipIdx+1:]
		}
		x.fcs = fcs
		x.fcsInfo = infos
		x.Next()
	} else {
		x.WriteHeader(404)
	}
}

// parsing logic
func parseSegment(seg string) (nodeType, string, *regexp.Regexp, []string) {
	if seg == "**" {
		return nodeCatchAll, "", nil, nil
	}
	if seg == "*" {
		return nodeWildcard, "", nil, nil
	}

	// Check for {param} or regex
	// If no { and no *, it's static
	if !strings.ContainsAny(seg, "{*") {
		return nodeStatic, "", nil, nil
	}

	// Complex parsing
	// {filepath:*} -> CatchAll with name "filepath"
	if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, ":*}") {
		name := seg[1 : len(seg)-3]
		return nodeCatchAll, name, nil, nil
	}

	// {name} -> Param
	if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") && strings.Count(seg, "{") == 1 {
		inner := seg[1 : len(seg)-1]
		if !strings.Contains(inner, ":") {
			return nodeParam, inner, nil, nil
		}
	}

	// Regex or Composite
	// Convert to Regex
	// Escape static parts, convert {name} to (?P<name>[^/]+), {name:regex} to (?P<name>regex)
	reStr := "^"
	paramKeys := make([]string, 0)

	idx := 0
	n := len(seg)
	for idx < n {
		start := strings.IndexByte(seg[idx:], '{')
		if start == -1 {
			reStr += regexp.QuoteMeta(seg[idx:])
			break
		}
		start += idx
		reStr += regexp.QuoteMeta(seg[idx:start])

		end := -1
		// Find matching }
		balance := 1
		for i := start + 1; i < n; i++ {
			if seg[i] == '{' {
				balance++
			} else if seg[i] == '}' {
				balance--
				if balance == 0 {
					end = i
					break
				}
			}
		}

		if end == -1 {
			// Malformed? treat as static
			reStr += regexp.QuoteMeta(seg[start:])
			break
		}

		// Parse content inside {}
		content := seg[start+1 : end]
		colon := strings.IndexByte(content, ':')
		var name, pattern string
		if colon == -1 {
			name = content
			pattern = "[^/]+"
		} else {
			name = content[:colon]
			pattern = content[colon+1:]
		}

		reStr += fmt.Sprintf("(?P<%s>%s)", regexp.QuoteMeta(name), pattern) // pattern shouldn't be quoted? pattern is regex.
		// Wait, user provided regex in pattern. Don't quote pattern.
		// Name should be safe?

		// Correct way:
		// name: valid identifier?
		// pattern: regex
		reStr = reStr[:len(reStr)-len(fmt.Sprintf("(?P<%s>%s)", regexp.QuoteMeta(name), pattern))] // Undo append to fix logic

		reStr += fmt.Sprintf("(?P<%s>%s)", name, pattern)
		paramKeys = append(paramKeys, name)

		idx = end + 1
	}
	reStr += "$"

	re, err := regexp.Compile(reStr)
	if err != nil {
		logv.Error().Msgf("Invalid route regex: %s, %v", seg, err)
		return nodeStatic, "", nil, nil // Fallback
	}

	return nodeRegex, "", re, paramKeys
}

func (r *route) get_subrouter(path string) *route {
	if path == "" || path == "/" {
		return r
	}
	if path[0] == '/' {
		path = path[1:]
	}
	// Trim trailing slash
	if len(path) > 0 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}

	segments := strings.Split(path, "/")
	current := r

	for _, seg := range segments {
		// Parse segment type
		kind, pName, re, keys := parseSegment(seg)

		// Find matching child (Exact match for existing node logic?)
		// We need to find if we already have an equivalent node.
		var next *route
		for _, child := range current.children {
			if child.kind == kind && child.fragment == seg {
				next = child
				break
			}
		}

		if next == nil {
			next = &route{
				kind:      kind,
				fragment:  seg,
				paramName: pName,
				regex:     re,
				paramKeys: keys,
				children:  make([]*route, 0),
				parent:    current,
				methods:   make(map[string]*RouteHandler),
			}
			current.children = append(current.children, next)

			// Sort children: * and ** at the end.
			sort.SliceStable(current.children, func(i, j int) bool {
				ki := current.children[i].kind
				kj := current.children[j].kind
				// Priority: Static > Regex/Param > Wildcard/CatchAll?
				// User said: "Matched in order, * sorted at end".
				// So we only move Wildcard/CatchAll to end.
				isWildI := ki == nodeWildcard || ki == nodeCatchAll
				isWildJ := kj == nodeWildcard || kj == nodeCatchAll

				if isWildI && !isWildJ {
					return false
				}
				if !isWildI && isWildJ {
					return true
				}
				// Otherwise keep original order (stable sort)
				return i < j
			})
		}
		current = next
	}
	return current
}

func (r *route) Clear(prefix string, method string) {
	node := r.get_subrouter(prefix)
	if method == "*" {
		node.methods = make(map[string]*RouteHandler)
		node.funcBefore = nil
		node.funcAfter = nil
	} else {
		delete(node.methods, method)
	}
	node.syncCache()
}

func getHandlerLocation() (string, int) {
	depth := 1
	for {
		pc, file, line, ok := runtime.Caller(depth)
		depth++
		if !ok {
			break
		}
		funcName := runtime.FuncForPC(pc).Name()
		if !strings.HasPrefix(funcName, "github.com/veypi/vigo") || strings.HasSuffix(file, "_test.go") {
			return file, line
		}
	}
	return "", 0
}

// Set registers handlers for a specific route path and method.
// The handlers argument can contain multiple types of values:
//   - Functions: The actual route handlers (middleware or final handler).
//   - String: Treated as the API description/summary.
//   - Struct (or pointer to struct): Treated as the input parameter schema/description.
func (r *route) Set(prefix string, method string, handlers ...any) Router {
	method = strings.ToUpper(method)
	logv.Assert(slices.Contains(allowedMethods, method), fmt.Sprintf("not support HTTP method: %v", method))
	logv.Assert(len(handlers) > 0, "there must be at least one handler")

	node := r.get_subrouter(prefix)

	if node.methods == nil {
		node.methods = make(map[string]*RouteHandler)
	}

	desc := ""
	desarg := ""
	var args any
	var response any
	filterHandlers := make([]any, 0, len(handlers))
	filterHandlersInfo := make([]*HandlerInfo, 0, len(handlers))
	file, line := getHandlerLocation()

	for _, fc := range handlers {
		if _, ok := fc.(FuncErr); ok {
			filterHandlers = append(filterHandlers, fc)
			filterHandlersInfo = append(filterHandlersInfo, &HandlerInfo{
				Func:   fc,
				Name:   getFuncName(fc),
				File:   file,
				Line:   line,
				Scoped: "",
			})
			continue
		}
		if _, ok := fc.(FuncSkipBefore); ok {
			filterHandlers = append(filterHandlers, fc)
			filterHandlersInfo = append(filterHandlersInfo, &HandlerInfo{
				Func:   fc,
				Name:   getFuncName(fc),
				File:   file,
				Line:   line,
				Scoped: "",
			})
			continue
		}
		if s, ok := fc.(string); ok {
			desc = s
			continue
		}

		// try to standardize
		var std FuncX2AnyErr
		if s, ok := TryStandardize(fc); ok {
			std = s
			// add args description
			fct := reflect.TypeOf(fc)
			if fct.NumIn() == 2 {
				argType := fct.In(1)
				if argType.Kind() == reflect.Ptr {
					argType = argType.Elem()
				}
				if argType.Kind() == reflect.Struct {
					if args == nil {
						args = reflect.New(argType).Interface()
						for i := 0; i < argType.NumField(); i++ {
							field := argType.Field(i)
							desarg += fmt.Sprintf("%s    %v    '%v'\n", field.Name, field.Type, field.Tag)
						}
					}
				}
			}
			if fct.NumOut() > 0 && fct.NumOut() <= 2 {
				resType := fct.Out(0)
				if resType.Kind() == reflect.Ptr {
					resType = resType.Elem()
				}
				if resType.Kind() == reflect.Struct {
					if response == nil {
						response = reflect.New(resType).Interface()
					}
				}
			}
		} else {
			// reflect checks...
			args = fc
			fct := reflect.TypeOf(fc)
			if fct.Kind() == reflect.Ptr {
				fct = fct.Elem()
			}
			if fct.Kind() == reflect.Struct {
				for i := 0; i < fct.NumField(); i++ {
					field := fct.Field(i)
					desarg += fmt.Sprintf("%s    %v    '%v'\n", field.Name, field.Type, field.Tag)
				}
			} else {
				logv.WithNoCaller.Fatal().Caller(2).Msgf("handler type not support: %T", fc)
			}
			continue
		}

		filterHandlers = append(filterHandlers, std)
		filterHandlersInfo = append(filterHandlersInfo, &HandlerInfo{
			Func:   fc,
			Name:   getFuncName(fc),
			File:   file,
			Line:   line,
			Scoped: "",
		})
	}
	if node.methods[method] != nil {
		logv.WithNoCaller.Warn().Msgf("handler %s %s already exists", node.String(), method)
	}
	node.methods[method] = &RouteHandler{
		Handlers:     filterHandlers,
		HandlersInfo: filterHandlersInfo,
		Caller:       getCaller(),
		Desc:         desc,
		Args:         args,
		Response:     response,
		ArgsDesc:     desarg,
	}
	node.syncCache()
	return node
}

func getCaller() [3]string {
	depth := 1
	for {
		pc, file, line, ok := runtime.Caller(depth)
		depth++
		if !ok {
			break
		}
		funcName := runtime.FuncForPC(pc).Name()
		if !strings.HasPrefix(funcName, "github.com/veypi/vigo") {
			return [3]string{file, fmt.Sprintf("%d", line), funcName}
		}
	}
	return [3]string{"", "", ""}
}

func (r *route) Any(url string, handlers ...any) Router {
	return r.Set(url, "ANY", handlers...)
}

func (r *route) Get(url string, handlers ...any) Router {
	return r.Set(url, http.MethodGet, handlers...)
}

func (r *route) Post(url string, handlers ...any) Router {
	return r.Set(url, http.MethodPost, handlers...)
}

func (r *route) Head(url string, handlers ...any) Router {
	return r.Set(url, http.MethodHead, handlers...)
}

func (r *route) Put(url string, handlers ...any) Router {
	return r.Set(url, http.MethodPut, handlers...)
}

func (r *route) Patch(url string, handlers ...any) Router {
	return r.Set(url, http.MethodPatch, handlers...)
}

func (r *route) Delete(url string, handlers ...any) Router {
	return r.Set(url, http.MethodDelete, handlers...)
}

func (r *route) addMiddleware(m any, method string, file string, line int, scope string, before bool) {
	originalM := m
	if _, ok := m.(FuncErr); ok {
		// pass
	} else if _, ok := m.(FuncSkipBefore); ok {
		// pass
	} else {
		if s, ok := TryStandardize(m); ok {
			m = s
		}
	}

	info := &HandlerInfo{
		Func:   originalM,
		Name:   getFuncName(originalM),
		File:   file,
		Line:   line,
		Scoped: scope,
	}

	if method == "" {
		if before {
			r.funcBefore = append(r.funcBefore, m)
			r.funcBeforeInfo = append(r.funcBeforeInfo, info)
		} else {
			r.funcAfter = append(r.funcAfter, m)
			r.funcAfterInfo = append(r.funcAfterInfo, info)
		}
		r.syncCache()
	} else {
		if r.methods == nil {
			r.methods = make(map[string]*RouteHandler)
		}
		if r.methods[method] == nil {
			r.methods[method] = &RouteHandler{}
		}

		if before {
			r.methods[method].Handlers = append([]any{m}, r.methods[method].Handlers...)
			r.methods[method].HandlersInfo = append([]*HandlerInfo{info}, r.methods[method].HandlersInfo...)
		} else {
			r.methods[method].Handlers = append(r.methods[method].Handlers, m)
			r.methods[method].HandlersInfo = append(r.methods[method].HandlersInfo, info)
		}
	}
}

func (r *route) After(middleware ...any) Router {
	method := ""
	file, line := getHandlerLocation()
	scope := r.String()
	if scope == "" {
		scope = "/"
	}
	for _, m := range middleware {
		switch m := m.(type) {
		case string:
			method = strings.ToUpper(m)
			if !slices.Contains(allowedMethods, method) {
				logv.WithDeepCaller.Warn().Msgf("invalid method %s", method)
				method = ""
			}
		default:
			r.addMiddleware(m, method, file, line, scope, false)
		}
	}
	r.syncCache()
	return r
}

func (r *route) Use(middleware ...any) Router {
	method := ""
	file, line := getHandlerLocation()
	scope := r.String()
	if scope == "" {
		scope = "/"
	}
	for _, m := range middleware {
		switch m := m.(type) {
		case string:
			method = strings.ToUpper(m)
			if !slices.Contains(allowedMethods, method) {
				logv.WithDeepCaller.Warn().Msgf("invalid method %s", method)
				method = ""
			}
		default:
			r.addMiddleware(m, method, file, line, scope, true)
		}
	}
	r.syncCache()
	return r
}

func (r *route) syncCache() {
	r.handlersCache = make(map[string][]any)
	r.handlersInfoCache = make(map[string][]*HandlerInfo)
	before := make([]any, 0, 10)
	beforeInfo := make([]*HandlerInfo, 0, 10)
	after := make([]any, 0, 10)
	afterInfo := make([]*HandlerInfo, 0, 10)
	tmpr := r
	for tmpr != nil {
		before = append(before[:0], append(tmpr.funcBefore, before...)...)
		beforeInfo = append(beforeInfo[:0], append(tmpr.funcBeforeInfo, beforeInfo...)...)
		after = append(after, tmpr.funcAfter...)
		afterInfo = append(afterInfo, tmpr.funcAfterInfo...)
		tmpr = tmpr.parent
	}
	for k, mh := range r.methods {
		r.handlersCache[k] = append(append([]any{}, before...), mh.Handlers...)
		r.handlersCache[k] = append(r.handlersCache[k], after...)

		r.handlersInfoCache[k] = append(append([]*HandlerInfo{}, beforeInfo...), mh.HandlersInfo...)
		r.handlersInfoCache[k] = append(r.handlersInfoCache[k], afterInfo...)
	}

	for _, sub := range r.children {
		sub.syncCache()
	}
}

func (r *route) Extend(prefix string, subr Router) Router {
	return r.get_subrouter(prefix).Replace(subr)
}

func (r *route) Replace(subr Router) Router {
	logv.Assert(r.parent != nil, "root router can not replace")

	// Replace r in r.parent.children
	// subr must be a *route
	sub := subr.(*route)
	sub.fragment = r.fragment
	sub.parent = r.parent
	sub.kind = r.kind
	sub.paramName = r.paramName
	sub.regex = r.regex
	sub.paramKeys = r.paramKeys

	// Find index
	idx := -1
	for i, c := range r.parent.children {
		if c == r {
			idx = i
			break
		}
	}
	if idx != -1 {
		r.parent.children[idx] = sub
	}

	sub.syncCache()
	return sub
}

func (r *route) SubRouter(prefix string) Router {
	logv.Assert(prefix != "" && prefix != "/", "subrouter path can not be '' or '/'")
	return r.get_subrouter(prefix)
}
