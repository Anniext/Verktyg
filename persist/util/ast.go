package util

import (
	"errors"
	"fmt"
	"github.com/fatih/structtag"
	"go/ast"
	"go/constant"
	"strconv"
	"strings"
)

func evalBinaryExpr(expr *ast.BinaryExpr) (constant.Value, error) {

	xLit, ok := expr.X.(*ast.BasicLit)
	if !ok {
		return constant.MakeUnknown(), errors.New("left operand is not BasicLit")
	}

	yLit, ok := expr.Y.(*ast.BasicLit)
	if !ok {
		return constant.MakeUnknown(), errors.New("right operand is not BasicLit")
	}

	x := evalBasicLit(xLit)
	y := evalBasicLit(yLit)
	return constant.BinaryOp(x, expr.Op, y), nil
}

func evalBasicLit(expr *ast.BasicLit) constant.Value {
	return constant.MakeFromLiteral(expr.Value, expr.Kind, 0)
}

func GetFieldTag(field *ast.Field, key string) *structtag.Tag {
	if field.Tag == nil {
		return &structtag.Tag{}
	}

	s, _ := strconv.Unquote(field.Tag.Value)
	tags, err := structtag.Parse(s)
	if err != nil {
		fmt.Printf("parse tag string:%s failed:%v\n", field.Tag.Value, err)
		return &structtag.Tag{}
	}
	tag, err := tags.Get(key)
	if err != nil {
		return &structtag.Tag{}
	}

	return tag
}

func GetFieldTags(field *ast.Field) *structtag.Tags {
	if field.Tag == nil {
		return &structtag.Tags{}
	}

	s, _ := strconv.Unquote(field.Tag.Value)
	tags, err := structtag.Parse(s)
	if err != nil {
		fmt.Printf("parse tag string:%s failed:%v\n", field.Tag.Value, err)
		return &structtag.Tags{}
	}
	if tags == nil {
		fmt.Printf("Error parse nil tag string:%s\n", field.Names)
		panic(field.Names)
	}
	return tags
}

func IsIgnoreField(field *ast.Field) bool {
	for _, tag := range GetFieldTags(field).Tags() {
		if tag.Key == "xorm" && strings.Contains(tag.Name, "-") {
			return true
		}
	}
	return false
}

func GetFieldName(field *ast.Field) string {
	if len(field.Names) > 0 {
		return field.Names[0].Name
	}

	return ""
}

func GetFieldType(field *ast.Field) string {
	if v, ok := field.Type.(*ast.Ident); ok {
		return v.Name
	}
	return ""
}

func GetStructBuildInTypeFields(node ast.Node) []*ast.Field {
	var fields []*ast.Field
	nodeType, ok := node.(*ast.StructType)
	if !ok {
		return nil
	}
	for _, field := range nodeType.Fields.List {
		if _, ok := field.Type.(*ast.Ident); !ok {
			continue
		}
		if field.Type.(*ast.Ident).Obj != nil {
			continue
		}
		fields = append(fields, field)
	}

	return fields
}

func GetStructBuildOutTypeFields(node ast.Node) []*ast.Field {
	var fields []*ast.Field
	nodeType, ok := node.(*ast.StructType)
	if !ok {
		return nil
	}
	for _, field := range nodeType.Fields.List {
		if _, ok := field.Type.(*ast.Ident); ok {
			continue
		}
		//if field.Type.(*ast.Ident).Obj == nil {
		//	continue
		//}
		fields = append(fields, field)
	}

	return fields
}

func getConstValue(obj *ast.Object) string {
	if obj.Kind != ast.Con {
		panic(errors.New("getConstValue error, invalid kind " + obj.Kind.String()))
	}
	decl, declOk := obj.Decl.(*ast.ValueSpec)
	if !declOk {
		panic(errors.New("getConstValue error, invalid kind " + obj.Name))
	}
	switch v := decl.Values[0].(type) {
	case *ast.BasicLit:
		return v.Value
	case *ast.BinaryExpr:
		if value, err := evalBinaryExpr(v); err == nil {
			return value.String()
		}
	}
	panic(errors.New("getConstValue error, invalid decl values " + obj.Name))
}

func getFiledTypeRecursion(fieldType interface{}) (typeStr string) {
	switch t := fieldType.(type) {
	case *ast.MapType:
		// Key
		typeStr += "map["
		typeStr += getFiledTypeRecursion(t.Key)
		typeStr += "]"
		// Value
		typeStr += getFiledTypeRecursion(t.Value)
	case *ast.ArrayType:
		// Slice or Array
		typeStr += "["
		switch tLen := t.Len.(type) {
		case *ast.BasicLit:
			typeStr += tLen.Value
		case *ast.Ident:
			typeStr += getConstValue(tLen.Obj)
		}
		typeStr += "]"
		// Value
		typeStr += getFiledTypeRecursion(t.Elt)
	case *ast.Ident:
		typeStr += t.Name
	case *ast.StarExpr:
		typeStr += "*"
		typeStr += getFiledTypeRecursion(t.X)
	case *ast.SelectorExpr:
		typeStr += getFiledTypeRecursion(t.X)
		typeStr += "."
		typeStr += getFiledTypeRecursion(t.Sel)
	}

	return
}

func getFiledCopyRecursion(deep int, filedName string, fieldType interface{}) (blockStr string) {
	deepOldStr := strconv.Itoa(deep - 1)
	deepStr := strconv.Itoa(deep)
	if deep == 0 {
		switch t := fieldType.(type) {
		case *ast.MapType:
			blockStr += `
    for k0, v0 := range src.` + filedName + "{"

			blockStr += getFiledCopyRecursion(deep+1, filedName+"[k"+deepStr+"]", t.Value)

			blockStr += `
    }`
		case *ast.ArrayType:
			// Slice or Array
			//typeStr += "["
			//if t.Len != nil {
			//	typeStr += t.Len.(*ast.BasicLit).Value
			//}
			//typeStr += "]"
			//// Value
			//typeStr += getFiledTypeRecursion(t.Elt)
		case *ast.Ident:
			// 不会执行到这里
			blockStr += `
    dst.` + filedName + " = src." + filedName
		}

	} else {
		switch t := fieldType.(type) {
		case *ast.MapType:
			blockStr += `
    for k` + deepStr + `, v` + deepStr + ` := range ` + "v" + deepOldStr + "{"

			blockStr += getFiledCopyRecursion(deep+1, filedName+"[k"+deepStr+"]", t.Value)

			blockStr += `
    }`
		case *ast.ArrayType:
			// Slice or Array
			//typeStr += "["
			//if t.Len != nil {
			//	typeStr += t.Len.(*ast.BasicLit).Value
			//}
			//typeStr += "]"
			//// Value
			//typeStr += getFiledTypeRecursion(t.Elt)
		case *ast.Ident:
			blockStr += `
`
		}

	}

	return
}

func GetFiledDeepCopyString(field *ast.Field) (block string) {
	fieldName := GetFieldName(field)
	fieldType := getFiledTypeRecursion(field.Type)
	fmt.Println("@@@@@@@@dst = src@@@@@@@@@", fieldType)
	block += `
    dst.` + fieldName + " = make(" + fieldType + ")"

	block += getFiledCopyRecursion(0, fieldName, field.Type)

	return
}

func IsStruct(fieldType interface{}) bool {
	switch v := fieldType.(type) {
	case *ast.Ident:
		if v.Obj != nil && v.Obj.Decl != nil {
			if vv, ok := v.Obj.Decl.(*ast.TypeSpec); ok {
				if _, vvOk := vv.Type.(*ast.StructType); vvOk {
					return true
				}
			}
		}
	case *ast.StarExpr:
		return IsStruct(v.X)
	}
	return false
}

func IsPoint(fieldType interface{}) bool {
	switch fieldType.(type) {
	case *ast.StarExpr:
		return true
	}
	return false
}

func GetFieldTypeInfo(field *ast.Field) TypeInfo {
	info := TypeInfo{}
	isPk := false
	isNotNull := false
	for _, tag := range GetFieldTags(field).Tags() {
		if tag.Key == "xorm" {
			if strings.Contains(tag.Name, "pk") {
				isPk = true
			}
			if strings.Contains(tag.Name, "not null") {
				isNotNull = true
			}
			if strings.Contains(tag.Name, "notnull") {
				isNotNull = true
			}
		}
	}
	info.Name = getFiledTypeRecursion(field.Type)
	if strings.Contains(info.Name, "RWMap") {
		info.NeedInit = true
	}
	pos := strings.Index(info.Name, ".")
	if pos != -1 {
		info.DSPkg = DropStar(strings.Trim(info.Name[:pos], `"`))
	}
	info.IsStruct = IsStruct(field.Type)
	info.IsPoint = IsPoint(field.Type)
	info.IsPk = isPk
	//info.IsString = info.Name == "string" || info.Name == "*string"
	switch info.Name {
	case "bool", "*bool":
		info.IsBool = true
		info.Bit = 8
	case "int8", "*int8":
		info.IsInt8 = true
		info.Bit = 8
	case "int16", "*int16":
		info.IsInt = true
		info.Bit = 16
	case "int32", "*int32":
		info.IsInt = true
		info.Bit = 32
	case "int64", "*int64":
		info.IsInt = true
		info.Bit = 64
	case "int", "*int":
		info.IsInt = true
		info.Bit = 64
	case "uint8", "*uint8":
		info.IsInt8 = true
		info.Bit = 8
	case "uint16", "*uint16":
		info.IsInt = true
		info.Bit = 16
	case "uint32", "*uint32":
		info.IsInt = true
		info.Bit = 32
	case "uint64", "*uint64":
		info.IsInt = true
		info.Bit = 64
	case "float32", "*float32":
		info.IsFloat = true
		info.Bit = 32
	case "float64", "*float64":
		info.IsFloat = true
		info.Bit = 64
	case "string", "*string":
		info.IsString = true
	default:
		info.IsNotNull = isNotNull
	}

	return info
}

func DropStar(name string) string {
	if len(name) > 0 {
		if name[0] == '*' {
			return name[1:]
		} else {
			return name
		}
	} else {
		return name
	}
}
