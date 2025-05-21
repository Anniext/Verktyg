//go:generate go install
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"io/ioutil"
	"os"
	"reflect"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/imports"
)

var (
	out   = flag.String("o", "", "")
	pkg   = flag.String("pkg", "main", "")
	name  = flag.String("name", "Map", "")
	ex    = flag.Bool("ex", false, "")
	usage = `Usage: rwmap [options...] map[T1]T2

Options:
  -o         Specify file output. If none is specified, the name
             will be derived from the map type.
  -pkg       Package name to use in the generated code. If none is
             specified, the name will main.
  -name      Struct name to use in the generated code. If none is
             specified, the name will be Map.
`
)
var templateCode = `
package rwmap

import (
	"errors"
	"github.com/json-iterator/go"
	"sync"
)

// Map is like a sync.Map, Reduce GC scanning
type Map struct {
	data map[interface{}]interface{}
	mu   sync.RWMutex
}

func (m *Map) checkData() {
	if m.data == nil {
		m.data = map[interface{}]interface{}{}
	}
}

func (m *Map) Init() *Map {
	m.mu.Lock()
	m.data = map[interface{}]interface{}{}
	m.mu.Unlock()
	return m
}

func (m *Map) Change(newMap map[interface{}]interface{}) {
	m.mu.Lock()
	m.data = newMap
	m.mu.Unlock()
}

// Load returns the value stored in the map for a key, or nil if no
// value is present.
// The ok result indicates whether value was found in the map.
func (m *Map) Load(key interface{}) (value interface{}, ok bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok = m.data[key]
	return
}

// Store sets the value for a key.
func (m *Map) Store(key, value interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkData()
	m.data[key] = value
	return
}

// Stores sets the value for a key.
func (m *Map) Stores(keys, values []interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkData()
	for idx, key := range keys {
		m.data[key] = values[idx]
	}
	return
}

// StoreMap sets the value for a key.
func (m *Map) StoreMap(tmp map[interface{}]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkData()
	for key, value := range tmp {
		m.data[key] = value
	}
	return
}

// LoadOrStore returns the existing value for the key if present.
// Otherwise, it stores and returns the given value.
// The loaded result is true if the value was loaded, false if stored.
func (m *Map) LoadOrStore(key, value interface{}) (actual interface{}, loaded bool) {
	m.mu.RLock()
	if m.data == nil {
		m.mu.RUnlock()
		m.mu.Lock()
		m.checkData()
		m.mu.Unlock()
		m.mu.RLock()
	}
	actual, loaded = m.data[key]
	m.mu.RUnlock()
	if !loaded {
		m.mu.Lock()
		if actual, loaded = m.data[key]; !loaded {
			m.data[key] = value
			actual = value
		}
		m.mu.Unlock()
	}
	return
}

// LoadAndDelete deletes the value for a key, returning the previous value if any.
// The loaded result reports whether the key was present.
func (m *Map) LoadAndDelete(key interface{}) (value interface{}, loaded bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkData()
	value, loaded = m.data[key]
	if loaded {
		delete(m.data, key)
	}
	return
}

// Delete deletes the value for a key.
func (m *Map) Delete(key interface{}) {
	m.LoadAndDelete(key)
}

// Delete deletes the all value.
func (m *Map) DeleteAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkData()
	length := len(m.data)
	keys := make([]interface{}, length)
	idx := 0
	for key, _ := range m.data {
		keys[idx] = key
		idx++
	}
	for _, key := range keys {
		delete(m.data, key)
	}
}

// Range calls f sequentially for each key and value present in the map.
// If f returns false, range stops the iteration.
//
// Range does not necessarily correspond to any consistent snapshot of the Map's
// contents: no key will be visited more than once, but if the value for any key
// is stored or deleted concurrently, Range may reflect any mapping for that key
// from any point during the Range call.
//
// Range may be O(N) with the number of elements in the map even if f returns
// false after a constant number of calls.
func (m *Map) Range(f func(key, value interface{}) bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for key, value := range m.data {
		if !f(key, value) {
			break
		}
	}
	return
}

// Items return keys and values present in the map.
func (m *Map) Items() (keys, values []interface{}) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	length := len(m.data)
	keys = make([]interface{}, length)
	values = make([]interface{}, length)
	idx := 0
	for key, value := range m.data {
		keys[idx] = key
		values[idx] = value
		idx++
	}
	return
}

// ItemMap return keys and values present in the map.
func (m *Map) ItemMap() (tmp map[interface{}]interface{}) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	length := len(m.data)
	tmp = make(map[interface{}]interface{}, length)
	for key, value := range m.data {
		tmp[key] = value
	}
	return
}

func (m *Map) FromDB(data []byte) (err error) {
	if len(data) == 0 {
		m.Init()
		return nil
	}
	err = m.UnmarshalJSON(data)
	return
}

func (m *Map) ToDB() (data []byte, err error) {
	data, err = m.MarshalJSON()
	return
}

func (m *Map) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	ret, err := jsoniter.ConfigCompatibleWithStandardLibrary.Marshal(m.data)
	if err != nil {
		return nil, err
	}
	return ret, nil

}

func (m *Map) UnmarshalJSON(b []byte) error {
	if m == nil {
		return errors.New(" Unmarshal(non-pointer MapInt32Int8)")
	}
	tmp := map[interface{}]interface{}{}
	err := jsoniter.ConfigCompatibleWithStandardLibrary.Unmarshal(b, &tmp)
	if err != nil {
		return err
	}
	if tmp == nil {
		tmp = map[interface{}]interface{}{}
	}
	m.Change(tmp)
	return nil
}

func (m *Map) String() string {
	if m == nil {
		return "{}"
	}
	data, err := m.MarshalJSON()
	if err != nil {
		return "{}"
	}
	return string(data)
}
`

var templateCodeExTrue = `

// AddStore add the value for a key.
func (m *Map) AddStore(key, value interface{}) (ret interface{}) {
	m.mu.Lock()
	m.checkData()
	ret = m.data[key]
    ret += value
	m.data[key] = ret
	m.mu.Unlock()
	return
}

// AddStores add the values for a keys.
func (m *Map) AddStores(keys, values []interface{}) {
	m.mu.Lock()
	m.checkData()
	for i, key := range keys {
		m.data[key] += values[i]
	}
	m.mu.Unlock()
	return
}

`

var templateCodeExFalse = `

// AddStore add the value for a key.
func (m *Map) AddStore(key, value interface{}) {
	panic("Not Implemented")
	return
}

// AddStores add the values for a keys.
func (m *Map) AddStores(key, value []interface{}) {
	panic("Not Implemented")
	return
}

`

func main() {
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, fmt.Sprintf(usage))
	}
	flag.Parse()
	g, err := NewGenerator()
	failOnErr(err)
	err = g.Mutate()
	failOnErr(err)
	err = g.Gen()
	failOnErr(err)
}

// Generator generates the typed rwmap object.
type Generator struct {
	// flag options.
	pkg   string // package name.
	out   string // file name.
	name  string // struct name.
	key   string // map key type.
	value string // map value type.
	// mutation state and traversal handlers.
	file   *ast.File
	fset   *token.FileSet
	funcs  map[string]func(*ast.FuncDecl)
	types  map[string]func(*ast.TypeSpec)
	values map[string]func(*ast.ValueSpec)
}

// NewGenerator returns a new generator for rwmap.
func NewGenerator() (g *Generator, err error) {
	defer catch(&err)
	g = &Generator{fset: token.NewFileSet(), pkg: *pkg, out: *out, name: *name}
	g.funcs = g.Funcs()
	g.types = g.Types()
	g.values = g.Values()
	exp, err := parser.ParseExpr(os.Args[len(os.Args)-1])
	check(err, "parse expr: %s", os.Args[len(os.Args)-1])
	m, ok := exp.(*ast.MapType)
	expect(ok, "invalid argument. expected map[T1]T2")
	b := bytes.NewBuffer(nil)
	err = format.Node(b, g.fset, m.Key)
	check(err, "format map key")
	g.key = b.String()
	b.Reset()
	err = format.Node(b, g.fset, m.Value)
	check(err, "format map value")
	g.value = b.String()
	if g.out == "" {
		g.out = "001_" + strings.ToLower(g.name) + ".go"
	}
	return
}

// Mutate mutates the original `sync/map` AST and brings it to the desired state.
// It fails if it encounters an unrecognized node in the AST.
func (g *Generator) Mutate() (err error) {
	defer catch(&err)
	//path := fmt.Sprintf("./rwmap/rwmap/rwmap.go")
	//b, err := ioutil.ReadFile(path)
	//check(err, "read %q file", path)
	if *ex {
		templateCode = templateCode + templateCodeExTrue
	} else {
		templateCode = templateCode + templateCodeExFalse
	}
	f, err := parser.ParseFile(g.fset, "", templateCode, parser.ParseComments)
	//check(err, "parse %q file", path)
	f.Name.Name = g.pkg
	astutil.AddImport(g.fset, f, "sync")
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			handler, ok := g.funcs[d.Name.Name]
			expect(ok, "unrecognized function: %s", d.Name.Name)
			handler(d)
			delete(g.funcs, d.Name.Name)
		case *ast.GenDecl:
			switch d := d.Specs[0].(type) {
			case *ast.TypeSpec:
				handler, ok := g.types[d.Name.Name]
				expect(ok, "unrecognized type: %s", d.Name.Name)
				handler(d)
				delete(g.types, d.Name.Name)
			case *ast.ValueSpec:
				handler, ok := g.values[d.Names[0].Name]
				expect(ok, "unrecognized value: %s", d.Names[0].Name)
				handler(d)
				expect(len(d.Names) == 1, "mismatch values length: %d", len(d.Names))
				delete(g.values, d.Names[0].Name)
			}
		default:
			expect(false, "unrecognized type: %s", d)
		}
	}
	expect(len(g.funcs) == 0, "function was deleted")
	expect(len(g.types) == 0, "type was deleted")
	expect(len(g.values) == 0, "value was deleted")
	rename(f, map[string]string{
		"Map":      g.name,
		"entry":    "entry" + strings.Title(g.name),
		"readOnly": "readOnly" + strings.Title(g.name),
		"expunged": "expunged" + strings.Title(g.name),
		"newEntry": "newEntry" + strings.Title(g.name),
	})
	g.file = f
	return
}

// Gen dumps the mutated AST to a file in the configured destination.
func (g *Generator) Gen() (err error) {
	defer catch(&err)
	b := bytes.NewBuffer([]byte("// Code generated by rwmap; DO NOT EDIT.\n\n"))
	err = format.Node(b, g.fset, g.file)
	check(err, "format mutated code")
	src, err := imports.Process(g.out, b.Bytes(), nil)
	check(err, "running goimports on: %s", g.out)
	err = ioutil.WriteFile(g.out, src, 0644)
	check(err, "writing file: %s", g.out)
	return
}

// Values returns all ValueSpec handlers for AST mutation.
func (g *Generator) Values() map[string]func(*ast.ValueSpec) {
	return map[string]func(*ast.ValueSpec){}
}

// Types returns all TypesSpec handlers for AST mutation.
func (g *Generator) Types() map[string]func(*ast.TypeSpec) {
	return map[string]func(*ast.TypeSpec){
		"Map": func(t *ast.TypeSpec) {
			l := t.Type.(*ast.StructType).Fields.List[0]
			g.renameMapType(l)
		},
	}
}

// Funcs returns all FuncDecl handlers for AST mutation.
func (g *Generator) Funcs() map[string]func(*ast.FuncDecl) {
	//nop := func(*ast.FuncDecl) {}
	return map[string]func(*ast.FuncDecl){
		"Init": func(f *ast.FuncDecl) {
			g.renameMapType(f.Body)
		},
		"checkData": func(f *ast.FuncDecl) {
			g.renameMapType(f.Body)
		},
		"Change": func(f *ast.FuncDecl) {
			g.renameMapType(f.Type.Params)
		},
		"Load": func(f *ast.FuncDecl) {
			g.replaceKey(f.Type.Params)
			g.replaceValue(f.Type.Results)
			renameNil(f.Body, f.Type.Results.List[0].Names[0].Name)
		},
		"Store": func(f *ast.FuncDecl) {
			g.renameTuple(f.Type.Params)
		},
		"AddStore": func(f *ast.FuncDecl) {
			g.renameTuple(f.Type.Params)
			g.replaceValue(f.Type.Results)
		},
		"AddStores": func(f *ast.FuncDecl) {
			g.renameTupleList(f.Type.Params)
		},
		"Stores": func(f *ast.FuncDecl) {
			g.renameTupleList(f.Type.Params)
		},
		"LoadOrStore": func(f *ast.FuncDecl) {
			g.renameTuple(f.Type.Params)
			g.replaceValue(f.Type.Results)
		},
		"LoadAndDelete": func(f *ast.FuncDecl) {
			g.replaceKey(f.Type.Params)
			g.replaceValue(f.Type.Results)
			renameNil(f.Body, f.Type.Results.List[0].Names[0].Name)
		},
		"Range": func(f *ast.FuncDecl) {
			g.renameTuple(f.Type.Params.List[0].Type.(*ast.FuncType).Params)
		},
		"Items": func(f *ast.FuncDecl) {
			g.renameTupleList(f.Type.Results)
			g.renameMapKeysValues(f.Body)
		},
		"ItemMap": func(f *ast.FuncDecl) {
			g.renameMapType(f.Type.Results)
			g.renameMapType(f.Body)
		},
		"StoreMap": func(f *ast.FuncDecl) {
			g.renameMapType(f.Type.Params)
			g.renameMapType(f.Body)
		},
		"Delete": func(f *ast.FuncDecl) { g.replaceKey(f) },
		"DeleteAll": func(f *ast.FuncDecl) {
			g.renameMapKeysValues(f.Body)
		},
		"FromDB":      func(f *ast.FuncDecl) {},
		"ToDB":        func(f *ast.FuncDecl) {},
		"MarshalJSON": func(f *ast.FuncDecl) {},
		"UnmarshalJSON": func(f *ast.FuncDecl) {
			g.renameMapType(f.Body)
		},
		"String": func(f *ast.FuncDecl) {},
	}
}

// replaceKey replaces all `interface{}` occurrences in the given Node with the key node.
func (g *Generator) replaceKey(n ast.Node) { replaceIface(n, g.key) }

// replaceValue replaces all `interface{}` occurrences in the given Node with the value node.
func (g *Generator) replaceValue(n ast.Node) { replaceIface(n, g.value) }

func (g *Generator) renameTuple(l *ast.FieldList) {
	if g.key == g.value {
		g.replaceKey(l.List[0])
		return
	}
	l.List = append(l.List, &ast.Field{
		Names: []*ast.Ident{l.List[0].Names[1]},
		Type:  l.List[0].Type,
	})
	l.List[0].Names = l.List[0].Names[:1]
	g.replaceKey(l.List[0])
	g.replaceValue(l.List[1])
}

func (g *Generator) renameTupleList(l *ast.FieldList) {
	if g.key == g.value {
		g.replaceKey(l.List[0])
		return
	}
	tmpArrayType := ast.ArrayType{}
	tmpArrayType = *l.List[0].Type.(*ast.ArrayType)
	l.List = append(l.List, &ast.Field{
		Names: []*ast.Ident{l.List[0].Names[1]},
		Type:  &tmpArrayType,
	})
	l.List[0].Names = l.List[0].Names[:1]
	g.replaceKey(l.List[0])
	g.replaceValue(l.List[1])
}

func (g *Generator) renameMapType(n ast.Node) {
	astutil.Apply(n, func(c *astutil.Cursor) bool {
		if v, ok := c.Node().(*ast.MapType); ok {
			_, isIFaceKey := v.Key.(*ast.InterfaceType)
			_, isIFaceValue := v.Value.(*ast.InterfaceType)
			if isIFaceKey && isIFaceValue {
				c.Replace(expr(fmt.Sprintf("map[%s]%s", g.key, g.value), v.Pos()))
			}
		}
		return true
	}, nil)
}

func (g *Generator) renameMapKeysValues(n ast.Node) {
	astutil.Apply(n, func(c *astutil.Cursor) bool {
		if v, ok := c.Node().(*ast.AssignStmt); ok {
			rhs, isCallRhs := v.Rhs[0].(*ast.CallExpr)
			if isCallRhs {
				if funName, isFun := rhs.Fun.(*ast.Ident); isFun && (funName.Name == "make") {
					if lhs, isIdentLhs := v.Lhs[0].(*ast.Ident); isIdentLhs {
						if lhs.Name == "keys" {
							// keys
							g.replaceKey(rhs.Args[0])
						} else {
							// values
							g.replaceValue(rhs.Args[0])
						}
					}
				}
			}
			//c.Replace(expr(fmt.Sprintf("map[%s]%s", g.key, g.value), v.Pos()))
		}
		return true
	}, nil)

}

func replaceIface(n ast.Node, s string) {
	astutil.Apply(n, func(c *astutil.Cursor) bool {
		n := c.Node()
		if it, ok := n.(*ast.InterfaceType); ok {
			c.Replace(expr(s, it.Interface))
		}
		return true
	}, nil)
}

func rename(f *ast.File, oldnew map[string]string) {
	astutil.Apply(f, func(c *astutil.Cursor) bool {
		switch n := c.Node().(type) {
		case *ast.Ident:
			if name, ok := oldnew[n.Name]; ok {
				n.Name = name
				n.Obj.Name = name
			}
		case *ast.FuncDecl:
			if name, ok := oldnew[n.Name.Name]; ok {
				n.Name.Name = name
			}
		}
		return true
	}, nil)
}

func renameNil(n ast.Node, name string) {
	astutil.Apply(n, func(c *astutil.Cursor) bool {
		if _, ok := c.Parent().(*ast.ReturnStmt); ok {
			if i, ok := c.Node().(*ast.Ident); ok && i.Name == new(types.Nil).String() {
				i.Name = name
			}
		}
		return true
	}, nil)
}

func expr(s string, pos token.Pos) ast.Expr {
	exp, err := parser.ParseExpr(s)
	check(err, "parse expr: %q", s)
	setPos(exp, pos)
	return exp
}

func setPos(n ast.Node, p token.Pos) {
	if reflect.ValueOf(n).IsNil() {
		return
	}
	switch n := n.(type) {
	case *ast.Ident:
		n.NamePos = p
	case *ast.MapType:
		n.Map = p
		setPos(n.Key, p)
		setPos(n.Value, p)
	case *ast.FieldList:
		n.Closing = p
		n.Opening = p
		if len(n.List) > 0 {
			setPos(n.List[0], p)
		}
	case *ast.Field:
		setPos(n.Type, p)
		if len(n.Names) > 0 {
			setPos(n.Names[0], p)
		}
	case *ast.FuncType:
		n.Func = p
		setPos(n.Params, p)
		setPos(n.Results, p)
	case *ast.ArrayType:
		n.Lbrack = p
		setPos(n.Elt, p)
	case *ast.StructType:
		n.Struct = p
		setPos(n.Fields, p)
	case *ast.SelectorExpr:
		setPos(n.X, p)
		n.Sel.NamePos = p
	case *ast.InterfaceType:
		n.Interface = p
		setPos(n.Methods, p)
	case *ast.StarExpr:
		n.Star = p
		setPos(n.X, p)
	case *ast.ChanType:
		setPos(n.Value, p)
	case *ast.ParenExpr:
		setPos(n.X, p)
	default:
		panic(fmt.Sprintf("unknown type: %v", n))
	}
}

// check panics if the error is not nil.
func check(err error, msg string, args ...interface{}) {
	if err != nil {
		args = append(args, err)
		panic(genError{fmt.Sprintf(msg+": %s", args...)})
	}
}

// expect panic if the condition is false.
func expect(cond bool, msg string, args ...interface{}) {
	if !cond {
		panic(genError{fmt.Sprintf(msg, args...)})
	}
}

type genError struct {
	msg string
}

func (p genError) Error() string { return fmt.Sprintf("rwmap: %s", p.msg) }

func catch(err *error) {
	if e := recover(); e != nil {
		gerr, ok := e.(genError)
		if !ok {
			panic(e)
		}
		*err = gerr
	}
}

func failOnErr(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n\n", err.Error())
		os.Exit(1)
	}
}
