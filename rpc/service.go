/**********************************************************\
|                                                          |
|                          hprose                          |
|                                                          |
| Official WebSite: http://www.hprose.com/                 |
|                   http://www.hprose.org/                 |
|                                                          |
\**********************************************************/
/**********************************************************\
 *                                                        *
 * rpc/service.go                                         *
 *                                                        *
 * hprose service for Go.                                 *
 *                                                        *
 * LastModified: Sep 15, 2016                             *
 * Author: Ma Bingyao <andot@hprose.com>                  *
 *                                                        *
\**********************************************************/

package rpc

import (
	"crypto/rand"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/hprose/hprose-golang/io"
	"github.com/hprose/hprose-golang/util"
)

// Service interface
type Service interface {
	AddFunction(name string, function interface{}, options Options) Service
	AddFunctions(names []string, functions []interface{}, options Options) Service
	AddMethod(name string, obj interface{}, options Options, alias ...string) Service
	AddMethods(names []string, obj interface{}, options Options, aliases ...[]string) Service
	AddInstanceMethods(obj interface{}, options Options) Service
	AddAllMethods(obj interface{}, options Options) Service
	AddMissingMethod(method MissingMethod, options Options) Service
	Remove(name string) Service
	Filter() Filter
	FilterByIndex(index int) Filter
	SetFilter(filter ...Filter) Service
	AddFilter(filter ...Filter) Service
	RemoveFilterByIndex(index int) Service
	RemoveFilter(filter ...Filter) Service
	AddInvokeHandler(handler InvokeHandler) Service
	AddBeforeFilterHandler(handler FilterHandler) Service
	AddAfterFilterHandler(handler FilterHandler) Service
}

type fixer interface {
	FixArguments(args []reflect.Value, context *ServiceContext)
}

func fixArguments(args []reflect.Value, context *ServiceContext) {
	i := len(args) - 1
	typ := args[i].Type()
	if typ == interfaceType || typ == contextType || typ == serviceContextType {
		args[i] = reflect.ValueOf(context)
	}
}

// BaseService is the hprose base service
type BaseService struct {
	*methodManager
	*handlerManager
	*filterManager
	fixer
	Event      ServiceEvent
	Debug      bool
	Simple     bool
	Timeout    time.Duration
	Heartbeat  time.Duration
	ErrorDelay time.Duration
	allTopics  map[string]map[string]*topic
}

// GetNextID is the default method for client uid
func GetNextID() (uid string) {
	u := make([]byte, 16)
	rand.Read(u)
	u[6] = (u[6] & 0x0f) | 0x40
	u[8] = (u[8] & 0x3f) | 0x80
	uid = fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:])
	return
}

// NewBaseService is the constructor for BaseService
func NewBaseService() (service *BaseService) {
	service = new(BaseService)
	service.methodManager = newMethodManager()
	service.handlerManager = newHandlerManager()
	service.filterManager = &filterManager{}
	service.Timeout = 120 * 1000 * 1000
	service.Heartbeat = 3 * 1000 * 1000
	service.ErrorDelay = 10 * 1000 * 1000
	service.allTopics = make(map[string]map[string]*topic)
	service.AddFunction("#", GetNextID, Options{Simple: true})
	service.override.invokeHandler = func(
		name string, args []reflect.Value,
		context Context) (results []reflect.Value, err error) {
		return invoke(name, args, context.(*ServiceContext))
	}
	service.override.beforeFilterHandler = func(
		request []byte, context Context) (response []byte, err error) {
		return service.beforeFilter(request, context.(*ServiceContext))
	}
	service.override.afterFilterHandler = func(
		request []byte, context Context) (response []byte, err error) {
		return service.afterFilter(request, context.(*ServiceContext))
	}
	return service
}

// AddFunction publish a func or bound method
// name is the method name
// function is a func or bound method
// options includes Mode, Simple, Oneway and NameSpace
func (service *BaseService) AddFunction(name string, function interface{}, options Options) Service {
	service.methodManager.AddFunction(name, function, options)
	return service
}

// AddFunctions is used for batch publishing service method
func (service *BaseService) AddFunctions(names []string, functions []interface{}, options Options) Service {
	service.methodManager.AddFunctions(names, functions, options)
	return service
}

// AddMethod is used for publishing a method on the obj with an alias
func (service *BaseService) AddMethod(name string, obj interface{}, options Options, alias ...string) Service {
	service.methodManager.AddMethod(name, obj, options, alias...)
	return service
}

// AddMethods is used for batch publishing methods on the obj with aliases
func (service *BaseService) AddMethods(names []string, obj interface{}, options Options, aliases ...[]string) Service {
	service.methodManager.AddMethods(names, obj, options, aliases...)
	return service
}

// AddInstanceMethods is used for publishing all the public methods and func fields with options.
func (service *BaseService) AddInstanceMethods(obj interface{}, options Options) Service {
	service.methodManager.AddInstanceMethods(obj, options)
	return service
}

// AddAllMethods will publish all methods and non-nil function fields on the
// obj self and on its anonymous or non-anonymous struct fields (or pointer to
// pointer ... to pointer struct fields). This is a recursive operation.
// So it's a pit, if you do not know what you are doing, do not step on.
func (service *BaseService) AddAllMethods(obj interface{}, options Options) Service {
	service.methodManager.AddAllMethods(obj, options)
	return service
}

// AddMissingMethod is used for publishing a method,
// all methods not explicitly published will be redirected to this method.
func (service *BaseService) AddMissingMethod(method MissingMethod, options Options) Service {
	service.methodManager.AddMissingMethod(method, options)
	return service
}

// Remove the published func or method by name
func (service *BaseService) Remove(name string) Service {
	service.methodManager.Remove(name)
	return service
}

// Filter return the first filter
func (service *BaseService) Filter() Filter {
	return service.filterManager.Filter()
}

// FilterByIndex return the filter by index
func (service *BaseService) FilterByIndex(index int) Filter {
	return service.filterManager.FilterByIndex(index)
}

// SetFilter will replace the current filter settings
func (service *BaseService) SetFilter(filter ...Filter) Service {
	service.filterManager.SetFilter(filter...)
	return service
}

// AddFilter add the filter to this Service
func (service *BaseService) AddFilter(filter ...Filter) Service {
	service.filterManager.AddFilter(filter...)
	return service
}

// RemoveFilterByIndex remove the filter by the index
func (service *BaseService) RemoveFilterByIndex(index int) Service {
	service.filterManager.RemoveFilterByIndex(index)
	return service
}

// RemoveFilter remove the filter from this Service
func (service *BaseService) RemoveFilter(filter ...Filter) Service {
	service.filterManager.RemoveFilter(filter...)
	return service
}

// AddInvokeHandler add the invoke handler to this Service
func (service *BaseService) AddInvokeHandler(handler InvokeHandler) Service {
	service.handlerManager.AddInvokeHandler(handler)
	return service
}

// AddBeforeFilterHandler add the filter handler before filters
func (service *BaseService) AddBeforeFilterHandler(handler FilterHandler) Service {
	service.handlerManager.AddBeforeFilterHandler(handler)
	return service
}

// AddAfterFilterHandler add the filter handler after filters
func (service *BaseService) AddAfterFilterHandler(handler FilterHandler) Service {
	service.handlerManager.AddAfterFilterHandler(handler)
	return service
}

func callService(
	name string, args []reflect.Value,
	context *ServiceContext) []reflect.Value {
	remoteMethod := context.Method
	function := remoteMethod.Function
	if context.IsMissingMethod {
		missingMethod := function.Interface().(MissingMethod)
		return missingMethod(name, args, context)
	}
	ft := function.Type()
	if ft.IsVariadic() {
		return function.CallSlice(args)
	}
	count := len(args)
	n := ft.NumIn()
	if n < count {
		args = args[:n]
	}
	return function.Call(args)
}

func doOutput(
	args []reflect.Value,
	results []reflect.Value,
	context *ServiceContext) []byte {
	method := context.Method
	writer := io.NewWriter(method.Simple)
	switch method.Mode {
	case RawWithEndTag:
		return results[0].Bytes()
	case Raw:
		writer.Write(results[0].Bytes())
	default:
		writer.WriteByte(io.TagResult)
		if method.Mode == Serialized {
			writer.Write(results[0].Bytes())
		} else {
			switch len(results) {
			case 0:
				writer.WriteNil()
			case 1:
				writer.WriteValue(results[0])
			default:
				writer.WriteSlice(results)
			}
		}
		if context.ByRef {
			writer.WriteByte(io.TagArgument)
			writer.Reset()
			writer.WriteSlice(args)
		}
	}
	return writer.Bytes()
}

func getErrorMessage(err error, debug bool) string {
	if panicError, ok := err.(*PanicError); ok {
		if debug {
			return fmt.Sprintf("%v\r\n%s", panicError.Panic, panicError.Stack)
		}
		return panicError.Error()
	}
	return err.Error()
}

func fireErrorEvent(event ServiceEvent, e error, context Context) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = NewPanicError(e)
		}
	}()
	err = e
	switch event := event.(type) {
	case sendErrorEvent:
		event.OnSendError(err, context)
	case sendErrorEvent2:
		err = event.OnSendError(err, context)
	}
	return err
}

func fireBeforeInvokeEvent(
	event ServiceEvent,
	name string,
	args []reflect.Value,
	context *ServiceContext) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = NewPanicError(e)
		}
	}()
	switch event := event.(type) {
	case beforeInvokeEvent:
		event.OnBeforeInvoke(name, args, context.ByRef, context)
	case beforeInvokeEvent2:
		err = event.OnBeforeInvoke(name, args, context.ByRef, context)
	}
	return err
}

func fireAfterInvokeEvent(
	event ServiceEvent,
	name string,
	args []reflect.Value,
	results []reflect.Value,
	context *ServiceContext) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = NewPanicError(e)
		}
	}()
	switch event := event.(type) {
	case afterInvokeEvent:
		event.OnAfterInvoke(name, args, context.ByRef, results, context)
	case afterInvokeEvent2:
		err = event.OnAfterInvoke(name, args, context.ByRef, results, context)
	}
	return err
}

func (service *BaseService) sendError(err error, context Context) []byte {
	err = fireErrorEvent(service.Event, err, context)
	w := io.NewWriter(true)
	w.WriteByte(io.TagError)
	w.WriteString(getErrorMessage(err, service.Debug))
	return w.Bytes()
}

func (service *BaseService) endError(err error, context Context) []byte {
	w := io.NewByteWriter(service.sendError(err, context))
	w.WriteByte(io.TagEnd)
	return w.Bytes()
}

func invoke(
	name string, args []reflect.Value,
	context *ServiceContext) (results []reflect.Value, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = NewPanicError(e)
		}
	}()
	if context.Oneway {
		go func() {
			defer recover()
			callService(name, args, context)
		}()
		return nil, nil
	}
	results = callService(name, args, context)
	n := len(results)
	if n == 0 {
		return results, nil
	}
	err, ok := results[n-1].Interface().(error)
	if ok {
		results = results[:n-1]
	}
	return results, err
}

func readArguments(
	fixer fixer,
	reader *io.Reader,
	method *Method,
	context *ServiceContext) (args []reflect.Value) {
	if method == nil {
		return reader.ReadSliceWithoutTag()
	}
	count := reader.ReadCount()
	ft := method.Function.Type()
	n := ft.NumIn()
	if ft.IsVariadic() {
		n--
	}
	max := util.Max(n, count)
	args = make([]reflect.Value, max)
	for i := 0; i < n; i++ {
		args[i] = reflect.New(ft.In(i)).Elem()
	}
	if n < count {
		if ft.IsVariadic() {
			for i := n; i < count; i++ {
				args[i] = reflect.New(ft.In(n)).Elem()
			}
		} else {
			for i := n; i < count; i++ {
				args[i] = reflect.New(interfaceType).Elem()
			}
		}
	}
	reader.ReadSlice(args[:count])
	if !ft.IsVariadic() && n > count {
		fixer.FixArguments(args, context)
	}
	return
}

func (service *BaseService) beforeInvoke(
	name string,
	args []reflect.Value,
	context *ServiceContext) (response []byte, err error) {
	err = fireBeforeInvokeEvent(service.Event, name, args, context)
	if err != nil {
		return nil, err
	}
	var results []reflect.Value
	results, err = service.handlerManager.invokeHandler(name, args, context)
	if err != nil {
		return nil, err
	}
	err = fireAfterInvokeEvent(service.Event, name, args, results, context)
	if err != nil {
		return nil, err
	}
	return doOutput(args, results, context), nil
}

func mergeResult(results [][]byte) []byte {
	n := len(results)
	if n == 1 {
		return append(results[0], io.TagEnd)
	}
	writer := io.NewByteWriter(results[0])
	for i := 1; i < n; i++ {
		writer.Write(results[i])
	}
	writer.WriteByte(io.TagEnd)
	return writer.Bytes()
}

func (service *BaseService) doSingleInvoke(
	reader *io.Reader, context *ServiceContext) (result []byte, tag byte) {
	name := reader.ReadString()
	alias := strings.ToLower(name)
	method := service.RemoteMethods[alias]
	tag = reader.CheckTags([]byte{io.TagList, io.TagEnd, io.TagCall})
	var args []reflect.Value
	if tag == io.TagList {
		reader.Reset()
		args = readArguments(service.fixer, reader, method, context)
		tag = reader.CheckTags([]byte{io.TagTrue, io.TagEnd, io.TagCall})
		if tag == io.TagTrue {
			context.ByRef = true
			tag = reader.CheckTags([]byte{io.TagEnd, io.TagCall})
		}
	}
	if method == nil {
		method = service.RemoteMethods["*"]
		context.IsMissingMethod = true
	}
	if method == nil {
		err := errors.New("Can't find this method " + name)
		return service.sendError(err, context), tag
	}
	context.Method = method
	result, err := service.beforeInvoke(name, args, context)
	if err != nil {
		return service.sendError(err, context), tag
	}
	return result, tag
}

func (service *BaseService) doInvoke(
	reader *io.Reader,
	context *ServiceContext) []byte {
	var results [][]byte
	for {
		result, tag := service.doSingleInvoke(reader, context)
		results = append(results, result)
		if tag != io.TagCall {
			break
		}
		reader.Reset()
	}
	return mergeResult(results)
}

func (service *BaseService) doFunctionList(context *ServiceContext) []byte {
	writer := io.NewWriter(true)
	writer.WriteByte(io.TagFunctions)
	writer.WriteStringSlice(service.MethodNames)
	writer.WriteByte(io.TagEnd)
	return writer.Bytes()
}

func (service *BaseService) afterFilter(
	request []byte,
	context *ServiceContext) (response []byte, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = NewPanicError(e)
		}
	}()
	reader := io.NewReader(request, false)
	tag, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	switch tag {
	case io.TagCall:
		return service.doInvoke(reader, context), nil
	case io.TagEnd:
		return service.doFunctionList(context), nil
	default:
		return nil, fmt.Errorf("Wrong Request: \r\n%s", request)
	}
}

func (service *BaseService) delayError(
	err error, context *ServiceContext) []byte {
	response := service.endError(err, context)
	if service.ErrorDelay > 0 {
		time.Sleep(service.ErrorDelay)
	}
	return response
}

func (service *BaseService) beforeFilter(
	request []byte, context *ServiceContext) (response []byte, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = NewPanicError(e)
		}
	}()
	request = service.inputFilter(request, context)
	response, err = service.afterFilterHandler(request, context)
	if err != nil {
		response = service.delayError(err, context)
	}
	return service.outputFilter(response, context), nil
}

// Handle the hprose request and return the hprose response
func (service *BaseService) Handle(request []byte, context Context) []byte {
	response, err := service.beforeFilterHandler(request, context)
	if err != nil {
		return service.endError(err, context)
	}
	return response
}

// func (service *BaseService) getTopics(topic string) (topics map[string]*topic) {
// 	topics = service.allTopics[topic]
// 	if topics == nil {
// 		panic(errors.New("topic \"" + topic + "\" is not published."))
// 	}
// 	return
// }

// func (service *BaseService) delTimer(topics map[string]*topic, id string) {
// 	t := topics[id]
// 	if t != nil && t.Timer != nil {
// 		t.Timer.Stop()
// 		t.Timer = nil
// 	}
// }

// func (service *BaseService) offline(
// 	topics map[string]*topic, topic string, id string) {
// 	service.delTimer(topics, id)

// }
