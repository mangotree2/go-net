package transport

import (
	"errors"
	"fmt"
	"github.com/henrylee2cn/teleport"
	"log"
	"path"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
	"unsafe"
)

type (
	Router struct {
		subRouter *SubRouter
	}

	SubRouter struct {
		root *Router
		callHandlers map[string]*Handler
		pushHandlers map[string]*Handler
		unknownCall **Handler
		unknownPush **Handler

		pathPrefix string
	}

	Handler struct {
		name string
		isUnknown bool
		argElem reflect.Type
		reply reflect.Type
		handleFunc func(*handlerCtx,reflect.Value)
		unKnownHandleFunc func( *handlerCtx)
		routerTypeName string
	}

	HandlersMasker func(string, interface{}) ([]*Handler,error)
)

const (
	pnPush = "PUSH"
	pnCall = "CALL"
	pnUnknownPush = "UNKNOWN_PUSH"
	pnUnknownCall = "UNKNOW_CALL"
)

func newRouter(rootGroup string) *Router {
	rootGroup = path.Join("/",rootGroup)
	root := &Router{
		subRouter:&SubRouter{
			callHandlers: make(map[string]*Handler),
			pushHandlers: make(map[string]*Handler),
			unknownCall: new(*Handler),
			unknownPush:new(*Handler),
			pathPrefix:rootGroup,
		},
	}

	root.subRouter.root = root

	return root

}

func (r *Router)SubRoute(pathPrefix string) *SubRouter {
	return r.subRouter.SubRoute(pathPrefix)
}

func (r *Router) RouteCall(CallCtrlStruct interface{}) []string {
	return r.subRouter.RouteCall(CallCtrlStruct)
}

func (r *Router) RouteCallFunc(callHandleFunc interface{}) string {
	return r.subRouter.RouteCallFunc(callHandleFunc)
}

func (r *Router) RoutePush(pushCtrlStruct interface{}) []string {
	return r.subRouter.RoutePush(pushCtrlStruct)
}

func (r *Router) RoutePushFunc(pushHandleFunc interface{}) string {
	return r.subRouter.RoutePushFunc(pushHandleFunc)
}

func (r *SubRouter)SubRoute(pathPrefix string) *SubRouter {
	//todo plugin
	return &SubRouter {
		root:r.root,
		callHandlers:r.callHandlers,
		pushHandlers:r.pushHandlers,
		unknownPush:r.unknownPush,
		unknownCall:r.unknownCall,
		pathPrefix:path.Join(r.pathPrefix,pathPrefix),
	}

}

func (r *SubRouter)RouteCall(callCtrlStruct interface{}) []string {
	return r.reg(pnCall,makeCallHandlersFromStruct,callCtrlStruct)
}

func (r *SubRouter)RouteCallFunc(callHandleFunc interface{}) string {
	return r.reg(pnCall,makeCallHanlersFromFunc,callHandleFunc)[0]
}

func (r *SubRouter)RoutePush(pushCtrlStruct interface{}) []string {
	return r.reg(pnPush,makePushHandlersFromStruct,pushCtrlStruct)
}

func (r *SubRouter)RoutePushFunc(pushHandleFunc interface{}) string {
	return r.reg(pnPush,makePushHandlersFromFunc,pushHandleFunc)[0]
}

func (r *SubRouter) getCall(uriPath string) (*Handler, bool) {
	t, ok := r.callHandlers[uriPath]
	if ok {
		return t, true
	}

	if unknown := *r.unknownCall; unknown != nil {
		return unknown,true
	}

	return nil,false
}

func (r *SubRouter)getPush(uriPath string) (*Handler,bool) {
	t ,ok := r.pushHandlers[uriPath]
	if ok {
		return t,true
	}

	if unkonwn := *r.unknownPush; unkonwn != nil {
		return unkonwn,true
	}
	return nil,false
}

func (r *Router)SetUnknownCall(fn func(UnknownCallCtx) (interface{}, error)) {

	var h = &Handler{
		name : pnUnknownCall,
		isUnknown: true,
		argElem: reflect.TypeOf([]byte{}),
		unKnownHandleFunc: func(ctx *handlerCtx) {
			body, err := fn(ctx)
			if err != nil {
				ctx.handlerErr =err
				//err.SetToMeta(ctx.output.Meta())
			} else {
				ctx.output.SetBody(body)
			}
		},
	}

	if *r.subRouter.unknownCall == nil {
		log.Printf("set handler :%s",h.name)
	} else{
		log.Printf("repeated unknowncall handler : %s",h.name)
	}
	r.subRouter.unknownCall = &h
}

func (r *Router) SetUnknownPush(fn func(UnknownPushCtx) error) {

	var h = &Handler{
		name:            pnUnknownPush,
		isUnknown:       true,
		argElem:         reflect.TypeOf([]byte{}),
		unKnownHandleFunc: func(ctx *handlerCtx) {
			ctx.handlerErr = fn(ctx)
		},
	}

	if *r.subRouter.unknownPush == nil {
		log.Printf("set %s handler", h.name)
	} else {
		log.Printf("repeated %s handler", h.name)
	}

	r.subRouter.unknownPush = &h
}


func (r *SubRouter)reg(routerType string, handlerMaker func(string, interface{}) ([]*Handler, error), ctrlStruct interface{}) []string {
	handlers,err := handlerMaker(r.pathPrefix,ctrlStruct)
	if err != nil {
		log.Fatalf("%v",err)
	}
	var names []string
	var hadHandlers map[string]*Handler
	if routerType == pnCall {
		hadHandlers = r.callHandlers
	} else {
		hadHandlers = r.pushHandlers
	}

	for _,h :=range handlers {
		if _,ok := hadHandlers[h.name]; ok {
			log.Fatalf("there is a handler conflict %s",h.name)
		}
		h.routerTypeName = routerType
		hadHandlers[h.name] = h

		names = append(names,h.name)
	}


	return names
}

func makeCallHandlersFromStruct(pathPrefix string, callCtrlStruct interface{}) ([]*Handler, error) {
	var (
		ctype = reflect.TypeOf(callCtrlStruct)
		handlers = make([]*Handler,0,1)
	)

	if ctype.Kind() != reflect.Ptr {
		return nil,errors.New("call-handler :the type is not struct Ptr,type is %s"+ctype.String())
	}

	var ctypeElem = ctype.Elem()
	if ctypeElem.Kind() != reflect.Struct {
		return nil ,errors.New("call-handler :the type is not struct type ：%s"+ctypeElem.String())
	}

	iType , ok := ctypeElem.FieldByName("CallCtx")
	if !ok || !iType.Anonymous {
		return nil,errors.New("call-handler :the sturct do not hanve anonymous CallCtx")
	}

	var callCtxOffset = iType.Offset

	type CallCtrlValue struct {
		ctrl reflect.Value
		ctxPtr *CallCtx

	}

	var pool = &sync.Pool{
		New: func() interface{} {
			ctrl := reflect.New(ctypeElem)
			callCtxPrt := ctrl.Pointer()+callCtxOffset
			ctxPtr := (*CallCtx)(unsafe.Pointer(callCtxPrt))
			return &CallCtrlValue{
				ctrl:ctrl,
				ctxPtr:ctxPtr,
			}
		},
	}

	for m := 0; m<ctype.NumMethod();m++ {
		method := ctype.Method(m)
		mtype := method.Type
		mname := method.Name
		//
		if method.PkgPath != "" {
			continue
		}

		if mtype.NumIn() != 2 {
			if isBelongToCallCtx(mname) {
				continue
			}
			return nil,errors.New("call-handler: just is need one arg, "+mname)
		}

		structType := mtype.In(0)
		if structType.Kind() != reflect.Ptr || structType.Elem().Kind() != reflect.Struct {
			if isBelongToCallCtx(mname) {
				continue
			}
			return nil,errors.New("call-handler: recevier nend be a pointer")
		}

		argType := mtype.In(1)
		if !IsExportedOrBuiltinType(argType) {
			if isBelongToCallCtx(mname) {
				continue
			}
			return nil,errors.New("call-handerl: arg tye not exported "+argType.String())
		}

		if argType.Kind() != reflect.Ptr {
			if isBelongToCallCtx(mname) {
				continue
			}
			return nil, errors.New("call-handler: art type need be a pointer")

		}

		if mtype.NumOut() != 2 {
			if isBelongToCallCtx(mname) {
				continue
			}
			return nil, errors.New("call-handler : nend two output arg ")
		}

		replyType := mtype.Out(0)
		if !IsExportedOrBuiltinType(replyType) {
			if isBelongToCallCtx(mname) {
				continue
			}
			return nil,errors.New("call-handler: first reply not a exprot")
		}

		if returnType := mtype.Out(1);!strings.HasSuffix(returnType.String(),"error") {
			if isBelongToCallCtx(mname) {
				continue
			}
			return nil,errors.New("call-handler: the second out arg need error"+returnType.String())
		}

		var methodFunc = method.Func
		var handlerFunc = func(ctx *handlerCtx,argValue reflect.Value) {
			obj := pool.Get().(*CallCtrlValue)
			*obj.ctxPtr = ctx
			rets := methodFunc.Call([]reflect.Value{obj.ctrl,argValue})
			err , _ := rets[1].Interface().(error)
			if err != nil {
				ctx.handlerErr = err
				//err.SetToMeta(ctx.output.Meta())
				ctx.output.Meta().SetErrToMeta(err.Error())

			} else {
				ctx.output.SetBody(rets[0].Interface())
			}

			pool.Put(obj)
		}

		handlers = append(handlers,&Handler{
			name:              path.Join(pathPrefix,ToUriPath(ctrlStructName(ctype)), ToUriPath(mname)),
			argElem:           argType.Elem(),
			reply:             replyType,
			handleFunc:        handlerFunc,
		})


	}

	return handlers,nil
}

func makeCallHanlersFromFunc(pathPrefix string, callHandlerFun interface{}) ([]*Handler, error) {
	var (
		ctype = reflect.TypeOf(callHandlerFun)
		cValue = reflect.ValueOf(callHandlerFun)
		typeString = objectName(cValue)
	)

	if ctype.Kind() != reflect.Func {
		return nil,errors.New("call-handler : tht type is not function"+typeString)
	}

	if ctype.NumOut() != 2 {
		return nil ,errors.New("call-handler : need two out arg,just have "+ fmt.Sprintf("%d",ctype.NumOut()))
	}

	replyType := ctype.Out(0)
	if !IsExportedOrBuiltinType(replyType) {
		return nil, errors.New("call-handler : first reply need export "+ctype.String())
	}

	if returnType := ctype.Out(1); !strings.HasSuffix(returnType.String(),"error") {
		return nil, errors.New("call-handler : the second output need error type "+returnType.String())
	}

	if ctype.NumIn() != 2 {
		return nil,errors.New("call-handler : the input need two arg")
	}

	argType := ctype.In(1)
	if !IsExportedOrBuiltinType(argType) {
		return nil, errors.New("call-handler : the arg type need export "+argType.String())
	}

	if argType.Kind() != reflect.Ptr {
		return nil, errors.New("call-handler : arg type need pointer "+argType.String())
	}

	//first arg need be a CallCtx
	ctxType := ctype.In(0)

	var handleFunc func(*handlerCtx,reflect.Value)

	switch ctype.Kind() {
	default:
		return nil,errors.New("call-handler : first arg must be callctx")
	case reflect.Interface :
		iface := reflect.TypeOf((*CallCtx)(nil)).Elem()
		if !ctxType.Implements(iface) ||
			!iface.Implements(reflect.New(ctxType).Type().Elem()) {
				return nil,errors.New("call-handler : first arg must be CallCtx type or struct pointer "+ ctxType.String())
		}

		handleFunc = func(ctx *handlerCtx, value reflect.Value) {
			rets := cValue.Call([]reflect.Value{reflect.ValueOf(ctx)})
			err , _ := rets[1].Interface().(error)
			if err != nil {
				ctx.handlerErr = err
				//err.SetToMeta(ctx.output.Meta())
			} else {
				ctx.output.SetBody(rets[0].Interface())
			}
		}
	case reflect.Ptr:
		var ctxTypeElem = ctxType.Elem()
		if ctxTypeElem.Kind() != reflect.Struct {
			return nil, errors.New("call-handler : first arg dmust be CallCtx type or struct pointer "+ctxTypeElem.String())
		}

		iType, ok := ctxTypeElem.FieldByName("CallCtx")
		if !ok || !iType.Anonymous {
			return nil, errors.New("call-handler : first arg do not have anonymous CallCtx"+ctxTypeElem.String())
		}

		type CallCtrlValue struct {
			ctrl reflect.Value
			ctxPrt *CallCtx
		}

		var callCtxOffset = iType.Offset

		var pool = sync.Pool{
			New: func() interface{} {
				ctrl := reflect.New(ctxTypeElem)
				callCtxPtr := ctrl.Pointer()+callCtxOffset
				ctxPrt := (*CallCtx)(unsafe.Pointer(callCtxPtr))
				return &CallCtrlValue{
					ctrl:ctrl,
					ctxPrt:ctxPrt,
				}
			},
		}

		handleFunc = func(ctx *handlerCtx,argValue reflect.Value) {
			obj := pool.Get().(*CallCtrlValue)
			*obj.ctxPrt = ctx
			rets := cValue.Call([]reflect.Value{obj.ctrl,argValue})
			err, _ := rets[1].Interface().(error)
			if err != nil {
				ctx.handlerErr = err
				//err.SetToMeta(ctx.output.Meta())
			} else {
				ctx.output.SetBody(rets[0].Interface())
			}
			pool.Get()
		}

	}

	return []*Handler{&Handler{
		name:              path.Join(pathPrefix,ToUriPath(handlerFuncName(cValue))),
		argElem:           argType.Elem(),
		reply:             replyType,
		handleFunc:        handleFunc,
	},
	},nil

}

func makePushHandlersFromStruct(pathPrefix string, pushCtrlStuct interface{}) ([]*Handler, error) {
	var (
		ctype = reflect.TypeOf(pushCtrlStuct)
		handlers = make([]*Handler,0,1)
	)

	if ctype.Kind() != reflect.Ptr {
		return nil ,errors.New("push-handler : the type is not struct point"+ctype.String())
	}

	var ctypeElem = ctype.Elem()
	if ctypeElem.Kind() != reflect.Struct {
		return nil, errors.New("push-handler : the type is not a struct "+ctypeElem.String())
	}

	iType , ok := ctypeElem.FieldByName("PushCtx")
	if !ok || !iType.Anonymous {
		return nil, errors.New("push-handler : the struct do not have anonymous PushCtx"+ctypeElem.String())
	}

	var pushCtxOffset = iType.Offset

	type PushCtrlValue struct {
		ctrl reflect.Value
		ctxPtr *PushCtx
	}

	var pool = &sync.Pool{
		New: func() interface{} {
			ctrl := reflect.New(ctypeElem)
			pushCtxPrt := ctrl.Pointer()+pushCtxOffset
			ctxPtr := (*PushCtx)(unsafe.Pointer(pushCtxPrt))
			return &PushCtrlValue{
				ctrl:ctrl,
				ctxPtr:ctxPtr,
			}
		},
	}

	for m := 0; m < ctype.NumMethod(); m++ {
		method := ctype.Method(m)
		mtype := method.Type
		mname := method.Name

		if method.PkgPath != "" {
			continue
		}

		if mtype.NumIn() != 2 {
			if isBelongToCallCtx(mname) {
				continue
			}
			return nil ,errors.New("push-handler : the func input arg num err "+mname)
		}

		structType := mtype.In(0)
		if structType.Kind() != reflect.Ptr || structType.Elem().Kind() != reflect.Struct {
			if isBelongToCallCtx(mname) {
				continue
			}
			return nil,errors.New("push-handler : the receiver need a struct pointer "+structType.String())
		}

		argType := mtype.In(1)
		if !IsExportedOrBuiltinType(argType) {
			if isBelongToCallCtx(mname) {
				continue
			}
			return nil,errors.New("push-handler : arg type need export "+argType.String())
		}

		if argType.Kind() != reflect.Ptr {
			if isBelongToCallCtx(mname) {
				continue
			}
			return nil, errors.New("push-handler : arg kind need pointer "+argType.String())
		}

		if mtype.NumOut() != 1 {
			if isBelongToCallCtx(mname) {
				continue
			}
			return nil, errors.New("push-handler : output arg just need error")
		}

		if returnType := mtype.Out(0);!strings.HasSuffix(returnType.String(),"error") {
			if isBelongToCallCtx(mname) {
				continue
			}
			return nil,errors.New("push-handler : output arg not a error "+returnType.String())
		}

		var methodFunc = method.Func
		var handleFunc = func(ctx *handlerCtx,argValue reflect.Value) {
			obj := pool.Get().(*PushCtrlValue)
			*obj.ctxPtr = ctx
			rets := methodFunc.Call([]reflect.Value{obj.ctrl,argValue})
			ctx.handlerErr, _ = rets[0].Interface().(error)
			pool.Put(obj)
		}

		handlers = append(handlers,&Handler{
			name: path.Join(pathPrefix,ToUriPath(ctrlStructName(ctype)), ToUriPath(mname)),
			handleFunc: handleFunc,
			argElem: argType.Elem(),
		})

	}

	return handlers, nil

}

func makePushHandlersFromFunc(pathPreFix string,pushHandleFunc interface{}) ([]*Handler, error) {
	var  (
		ctype = reflect.TypeOf(pushHandleFunc)
		cValue = reflect.ValueOf(pushHandleFunc)
		//typeString = objectName(cValue)
	)

	if ctype.Kind() != reflect.Func {
		return nil, errors.New("push-handler : the type is not a function "+ctype.Kind().String())
	}

	if ctype.NumOut() != 1 {
		return nil,errors.New("push-handler : the return just one arg")
	}

	if returnType := ctype.Out(0); !strings.HasSuffix(returnType.String(),"error") {
		return nil, errors.New("push-handler : the return must be error")
	}

	if ctype.NumIn() != 2 {
		return nil,errors.New("push-handler : the input arg need two arg")
	}

	argType := ctype.In(1)
	if ! IsExportedOrBuiltinType(argType) {
		return nil, errors.New("push-handler : the arg not export :"+argType.String())
	}

	if argType.Kind() != reflect.Ptr {
		return nil, errors.New("push-handler : arg type is not pointer "+argType.Kind().String())
	}

	ctxType := ctype.In(0)

	var handleFunc func( *handlerCtx, reflect.Value)

	switch ctxType.Kind() {
	default:
		return nil, errors.New("push-handler : the first arg must be PushCtx "+ctxType.Kind().String())

	case reflect.Interface:
		iface := reflect.TypeOf((*tp.PushCtx)(nil)).Elem()
		if !ctxType.Implements(iface) ||
			!iface.Implements(reflect.New(ctxType).Type().Elem()) {
				return nil, errors.New("push-handler : the first arg need implements PushCtx "+ctxType.String())
		}

		handleFunc = func(ctx *handlerCtx, value reflect.Value) {
			rets := cValue.Call([]reflect.Value{reflect.ValueOf(ctx),value})
			ctx.handlerErr , _ = rets[0].Interface().(error)
		}

	case reflect.Ptr:
		var ctxTypeElem = ctxType.Elem()
		if ctxTypeElem.Kind() != reflect.Struct {
			return nil, errors.New("push-handler : the first arg must be PushCtx"+ctxTypeElem.String())
		}
		iType ,ok := ctxTypeElem.FieldByName("PushCtx")
		if !ok || !iType.Anonymous {
			return nil, errors.New("push-handler : the first need have PushCtx "+ctxTypeElem.String())
		}

		type PushCtrlValue struct {
			ctrl reflect.Value
			ctxPtr *PushCtx
		}

		var pushCtxOffset = iType.Offset
		var pool = &sync.Pool{
			New: func() interface{} {
				ctrl := reflect.New(ctxTypeElem)
				pushCtxPtr := ctrl.Pointer() + pushCtxOffset
				ctxPrt := (*PushCtx)(unsafe.Pointer(pushCtxPtr))
				return &PushCtrlValue{
					ctrl:ctrl,
					ctxPtr:ctxPrt,
				}
			},
		}

		handleFunc = func(ctx *handlerCtx, value reflect.Value) {
			obj := pool.Get().(*PushCtrlValue)
			*obj.ctxPtr = ctx
			rets := cValue.Call([]reflect.Value{obj.ctrl,value})
			ctx.handlerErr ,_ = rets[0].Interface().(error)
			pool.Put(obj)
		}

	}

	return []*Handler{&Handler{
		name:path.Join(pathPreFix,ToUriPath(handlerFuncName(cValue))),
		handleFunc:handleFunc,
		argElem:argType.Elem(),
	}},nil

}

func isBelongToCallCtx(name string) bool {
	ctype := reflect.TypeOf(CallCtx(new(handlerCtx)))
	for m := 0; m < ctype.NumMethod(); m++ {
		if name == ctype.Method(m).Name {
			return true
		}
	}

	return false
}

func IsExportedOrBuiltinType(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	// PkgPath will be non-empty even for an exported type,
	// so we need to check the type name as well.
	return IsExportedName(t.Name()) || t.PkgPath() == ""
}

// IsExportedName is this an exported - upper case - name?
func IsExportedName(name string) bool {
	rune, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(rune)
}

func ctrlStructName(ctype reflect.Type) string {
	split := strings.Split(ctype.String(), ".")
	return split[len(split)-1]
}

func ToUriPath(name string) string {
	p := strings.Replace(name, "__", ".", -1)
	a := strings.Split(p, "_")
	for k, v := range a {
		a[k] = SnakeString(v)
	}
	p = path.Join(a...)
	p = path.Join("/", p)
	return strings.Replace(p, ".", "_", -1)
}

func SnakeString(s string) string {
	data := make([]byte, 0, len(s)*2)
	j := false
	for _, d := range []byte(s) {
		if d >= 'A' && d <= 'Z' {
			if j {
				data = append(data, '_')
				j = false
			}
		} else if d != '_' {
			j = true
		}
		data = append(data, d)
	}
	return strings.ToLower(string(data))
}

func objectName(v reflect.Value) string {
	t := v.Type()
	if t.Kind() == reflect.Func {
		return runtime.FuncForPC(v.Pointer()).Name()
	}
	return t.String()
}

func handlerFuncName(v reflect.Value) string {
	str := objectName(v)
	split := strings.Split(str, ".")
	return split[len(split)-1]
}

func (h *Handler) Name() string {
	return h.name
}

func (h *Handler) ArgElemType() reflect.Type {
	return h.argElem
}

func (h *Handler) NewArgValue() reflect.Value {
	return reflect.New(h.argElem)
}

func (h *Handler)ReplyType() reflect.Type {
	return h.reply
}

func (h *Handler) IsCall() bool {
	return h.routerTypeName == pnCall || h.routerTypeName == pnUnknownCall
}

func (h *Handler) IsPush() bool {
	return h.routerTypeName == pnPush || h.routerTypeName == pnUnknownPush
}

func (h *Handler) IsUnkonw() bool {
	return h.isUnknown
}

func (h *Handler) RouterTyoeName() string {
	return h.routerTypeName
}

