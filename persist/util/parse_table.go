package util

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"runtime/debug"
	"strings"
)

const (
	EWarningFlagUnloadFunction = 1 << iota
	EWarningFlagMax
)

const (
	// EOptimizeFlagIndexMutex 保证所有的索引值完全一致, 不开启则保证最终一致性(不建议开启!!! 串行化所有的索引操作, 影响并发量)
	EOptimizeFlagIndexMutex = 1 << iota
	// EOptimizeFlagSQLMerge 保证数据库索引关系完全一致, 不开启则保证最终一致性(建议开启!!! 内存中模拟合并, 性能影响不大, 减轻数据库开销)
	EOptimizeFlagSQLMerge
	// EOptimizeFlagGenerateCode 使用生成代码取代反射, 固定的模型写法, 不保证所有情况正确(强制开启!!! 解决20%的反射开销, 生成代码不一定考虑到了所有情况, 特殊写法需要先测试)
	EOptimizeFlagGenerateCode
	// EOptimizeFlagSQLInsertMerge 合并插入语句, 减少数据库IO, 减少反射次数(建议开启!!! 插入语句越多, 性能提升越明显, 插入非常少,可以不开启,减少cpu消耗)
	EOptimizeFlagSQLInsertMerge
	// EOptimizeFlagTraceSwitch 数据变化追踪开关(酌情开启!!! 大量日志影响性能，开启时请使用MarkUpdateByBitSet不要用MarkUpdate， 减少序列化开销)
	EOptimizeFlagTraceSwitch
	// EOptimizeFlagUsePoolAndDisableDeleteUnload 使用对象池并禁用删除接口(特殊情况开启!!! 行数大于5M且没有删除行为开启, 承降低载, 减少GC耗时, 减少内存)
	EOptimizeFlagUsePoolAndDisableDeleteUnload
	EOptimizeFlagMax
)

// {{index .HashIndexUnload.Cols 0}} {{index .HashIndexUnload.Types 0}}

type Index struct {
	Cols            []string
	Keys            string
	ClsPointKeys    string
	CommaKeys       string
	CommaKeyKeys    string
	KeyTypes        string
	Types           []string
	Unique          bool
	Pk              bool
	RBTreeValue     bool
	KeyTypesStripPk string
	KeysStripPk     []string
	EffectIndex     []*Index
}

func (i *Index) String() string {
	return fmt.Sprintf("{Index Cols=%s, Types=%s, Unique=%t, Pk=%t}\t", i.Cols, i.Types, i.Unique, i.Pk)
}

func (i *Index) StringDetail() string {
	return fmt.Sprintf(`{Index
 Cols=%s,
 Keys=%s,
 ClsPointKeys=%s,
 CommaKeys=%s,
 KeyTypes=%s,
 Types=%s,
 Unique=%t,
 Pk=%t
}`, i.Cols, i.Keys, i.ClsPointKeys, i.CommaKeys,
		i.KeyTypes, i.Types, i.Unique, i.Pk)
}

type ArgsInfo struct {
	Save                bool
	PersistPkgPath      string
	PersistPkgName      string
	UnloadKey           string
	Unload              bool
	WarningFlag         int64
	BombDir             string
	OptimizeFlag        int64
	MaxInsertRows       int64
	QueueThreshold      int64
	QueueEmptySleepTime int64
	FileName            string
	NeedSeparate        bool
	SwitchTable         string
	RBTreeCap           int32
}

func (args *ArgsInfo) OptimizeFlagIndexMutex() bool {
	return args.OptimizeFlag&EOptimizeFlagIndexMutex != 0
}
func (args *ArgsInfo) OptimizeFlagSQLMerge() bool {
	return args.OptimizeFlag&EOptimizeFlagSQLMerge != 0
}
func (args *ArgsInfo) OptimizeFlagGenerateCode() bool {
	return args.OptimizeFlag&EOptimizeFlagGenerateCode != 0
}
func (args *ArgsInfo) OptimizeFlagSQLInsertMerge() bool {
	return args.OptimizeFlag&EOptimizeFlagSQLInsertMerge != 0
}
func (args *ArgsInfo) OptimizeFlagTraceSwitch() bool {
	return args.OptimizeFlag&EOptimizeFlagTraceSwitch != 0
}

func (args *ArgsInfo) OptimizeFlagUsePoolAndDisableDeleteUnload() bool {
	return args.OptimizeFlag&EOptimizeFlagUsePoolAndDisableDeleteUnload != 0
}

func ValidWarningFlag(value, flag int64) bool {
	return (value & flag) == 1
}

type TypeInfo struct {
	DSPkg     string // 依赖的ds 包名
	Name      string // 类型名
	IsBool    bool
	IsInt8    bool
	IsInt     bool
	IsFloat   bool
	IsString  bool
	IsPoint   bool
	IsStruct  bool
	Bit       int
	IsPk      bool
	IsNotNull bool
	NeedInit  bool
	//IsMap       bool
}

func (i TypeInfo) String() string {
	return fmt.Sprintf("{TypeInfo IsInt=%6t, IsPoint=%6t, IsStruct=%6t, IsString=%6t, "+
		"Bit=%3d, IsFloat=%6t, IsBool=%6t, Name=%s, }",
		i.IsInt, i.IsPoint, i.IsStruct, i.IsString,
		i.Bit, i.IsFloat, i.IsBool, i.Name)
}

// MyVisitor indexList [{"cols":[key1, key2], "types":[type1, type2], "unique"=true "pk"=true},... ]
type MyVisitor struct {
	Depth               int
	DataName            string
	PackageName         string
	DataTypeSpec        *ast.TypeSpec
	DataStructType      *ast.StructType
	HashIndexList       []*Index
	RBTreeIndexList     []*Index
	ModifyHashIndexList []*Index
	ModifyColList       []string
	ColEffectHashMap    map[string][]*Index
	KeyTypeMap          map[string]string
	HashIndexUnload     *Index
	HashIndexPk         *Index
	IndexRBTreeValue    *Index
	ArgsInfo            *ArgsInfo
	FieldNameList       []string
	FieldTypeList       []TypeInfo
	DSImportsPackage    map[string]string // ds结构依赖的其他包 map[pkgName]pkgPath
	DSImport            string            // ds结构依赖的包名
}

func (v *MyVisitor) HasGlobalFuncNewPersist() bool {
	has := true
	if v.HashIndexPk != nil && v.ArgsInfo.Save && v.HashIndexUnload != nil {
		exist := false
		for _, col := range v.HashIndexPk.Cols {
			if col == v.HashIndexUnload.Cols[0] {
				exist = true
			}
		}
		if !exist {
			has = false
		}
	}
	return has
}

func (v *MyVisitor) IsGlobalPersist() bool {
	if v.HashIndexPk != nil && v.ArgsInfo.Save {
		pos := strings.Index(v.DataName, "Global")
		if pos != -1 {
			return true
		}
	}
	return false
}

func (v *MyVisitor) HaveRBTree() bool {
	return !(len(v.RBTreeIndexList) == 0)
}

func (v *MyVisitor) ReverseHashIndexList() (lst []*Index) {
	for i := len(v.HashIndexList) - 1; i >= 0; i-- {
		lst = append(lst, v.HashIndexList[i])
	}
	return
}

func (v *MyVisitor) DropStar(name string) string {
	return DropStar(name)
}

func (v *MyVisitor) IsRBTreeValue(c string) bool {
	for _, col := range v.IndexRBTreeValue.Cols {
		if col == c {
			return true
		}
	}
	return false
}

func ParseTable(filePathAbs string, tableName string, argsInfo *ArgsInfo) (visitor *MyVisitor) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("ParseTable panic", tableName, r)
			fmt.Println("stack: ", string(debug.Stack()))
		}
	}()

	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, filePathAbs, nil, parser.AllErrors|parser.ParseComments)
	if err != nil {
		fmt.Println(err.Error())
	}
	//ast.Print(fs, f)
	visitor = &MyVisitor{}
	visitor.DataName = tableName
	visitor.ArgsInfo = argsInfo
	visitor.DSImportsPackage = make(map[string]string)

	ast.Walk(visitor, f)

	//fmt.Println(visitor)

	indexDict := make(map[string]*Index)
	rbtreeIndexDict := make(map[string]*Index)
	var keyNameList []string
	var rbtreeKeyNameList []string

	visitor.KeyTypeMap = make(map[string]string)
	visitor.ColEffectHashMap = make(map[string][]*Index)

	// 提取所有列名 和 类型
	for _, field := range visitor.DataStructType.Fields.List {
		if IsIgnoreField(field) {
			continue
		}
		visitor.FieldNameList = append(visitor.FieldNameList, GetFieldName(field))
		visitor.FieldTypeList = append(visitor.FieldTypeList, GetFieldTypeInfo(field))
		//fmt.Println(GetFieldTypeInfo(field))
	}
	dsPkgMap := map[string]bool{}
	for _, info := range visitor.FieldTypeList {
		if (info.DSPkg != "") && info.IsNotNull {
			if _, exist := dsPkgMap[info.DSPkg]; exist {
				continue
			}
			dsPkgMap[info.DSPkg] = true
			visitor.DSImport += visitor.DSImportsPackage[info.DSPkg]
			visitor.DSImport += "\n"
		}
	}

	//for i, info := range visitor.FieldTypeList {
	//	if info.IsNotNull {
	//		dsImportMap[]
	//		info.
	//	}
	//
	//}

	// 遍历所有的tag, 提取索引信息
	for _, field := range GetStructBuildInTypeFields(visitor.DataStructType) {
		if _, ok := field.Type.(*ast.Ident); !ok {
			continue
		}
		//fmt.Println(GetFieldName(field))
		//fmt.Println(GetFieldType(field))
		//fmt.Println(GetFieldTags(field))
		var isPk bool
		var isUnique bool
		var index *Index
		var ok bool
		for _, tag := range GetFieldTags(field).Tags() {
			if tag.Key == "xorm" && strings.Contains(tag.Name, "pk") {
				isPk = true
			}
		}

		for _, tag := range GetFieldTags(field).Tags() {
			index = nil
			keyName := tag.Key
			if strings.Contains(tag.Key, "hash") {
				items := strings.Split(tag.Name, ";")
				for _, item := range items {
					kvs := strings.Split(item, "=")
					switch kvs[0] {
					case "unique":
						isUnique = kvs[1] == "1"
					case "pk":
						isPk = kvs[1] == "1"
					case "group":
						keyName += kvs[1]
					}
				}
				if index, ok = indexDict[keyName]; !ok {
					keyNameList = append(keyNameList, keyName)
					index = &Index{}
					indexDict[keyName] = index
				}
				if _, ok = visitor.KeyTypeMap[GetFieldName(field)]; !ok {
					visitor.KeyTypeMap[GetFieldName(field)] = GetFieldType(field)
					visitor.ModifyColList = append(visitor.ModifyColList, GetFieldName(field))
				}

				index.Cols = append(index.Cols, GetFieldName(field))
				index.Keys += GetFieldName(field)
				index.ClsPointKeys += "cls." + GetFieldName(field) + ","
				index.CommaKeys += GetFieldName(field) + ","
				index.CommaKeyKeys += GetFieldName(field) + ": " + GetFieldName(field) + ","
				index.KeyTypes += GetFieldName(field) + " " + GetFieldType(field) + ","
				index.Types = append(index.Types, GetFieldType(field))
				if index != nil {
					index.Pk = isPk
					index.Unique = isUnique
				}
			} else {
			}
			if strings.Contains(tag.Key, "rbtree") {

				var rbtreeIndex *Index
				if rbtreeIndex, ok = rbtreeIndexDict[keyName]; !ok {
					rbtreeKeyNameList = append(rbtreeKeyNameList, keyName)
					rbtreeIndex = &Index{}
					rbtreeIndexDict[keyName] = rbtreeIndex
				}

				items := strings.Split(tag.Name, ";")
				isRBTreeValue := false
				for _, item := range items {
					//kvs := strings.Split(item, "=")
					switch item {
					case "value":
						isRBTreeValue = true
					case "v":
						isRBTreeValue = true
					}
				}
				if _, ok = visitor.KeyTypeMap[GetFieldName(field)]; !ok {
					visitor.KeyTypeMap[GetFieldName(field)] = GetFieldType(field)
					visitor.ModifyColList = append(visitor.ModifyColList, GetFieldName(field))
				}

				rbtreeIndex.Cols = append(rbtreeIndex.Cols, GetFieldName(field))
				rbtreeIndex.Keys += GetFieldName(field)
				rbtreeIndex.ClsPointKeys += "cls." + GetFieldName(field) + ","
				rbtreeIndex.CommaKeys += GetFieldName(field) + ","
				rbtreeIndex.CommaKeyKeys += GetFieldName(field) + ": " + GetFieldName(field) + ","
				rbtreeIndex.KeyTypes += GetFieldName(field) + " " + GetFieldType(field) + ","
				rbtreeIndex.Types = append(rbtreeIndex.Types, GetFieldType(field))
				if rbtreeIndex != nil {
					rbtreeIndex.RBTreeValue = isRBTreeValue
				}
			}
			//fmt.Println(tag.Key, "@", tag.Name)
		}
	}
	//fmt.Println(keyNameList)

	// 主键强制放到第一个
	for _, keyName := range keyNameList {
		index := indexDict[keyName]
		// 记录主键信息
		if index.Pk && index.Unique {
			if visitor.HashIndexPk == nil {
				visitor.HashIndexPk = index
				// 主键移除修改列表
				for _, col := range index.Cols {
					for i := range visitor.ModifyColList {
						if visitor.ModifyColList[i] == col {
							visitor.ModifyColList = append(visitor.ModifyColList[:i], visitor.ModifyColList[i+1:]...)
							break
						}
					}
				}
			} else {
				panic("repeated pk " + tableName + visitor.HashIndexPk.String() + " " + index.String())
			}
		} else {
			// 处理非主键的索引修改
			visitor.HashIndexList = append(visitor.HashIndexList, index)
		}

	}

	for _, rbtreeKeyName := range rbtreeKeyNameList {
		rbtreeIndex := rbtreeIndexDict[rbtreeKeyName]
		if rbtreeIndex.RBTreeValue {
			// 是红黑树的value
			if visitor.IndexRBTreeValue == nil {
				visitor.IndexRBTreeValue = rbtreeIndex
				visitor.RBTreeIndexList = append(visitor.RBTreeIndexList, rbtreeIndex)
			}
		}
	}

	funcNotInIndex := func(col string, index *Index) bool {
		exist := false
		for _, colPk := range index.Cols {
			if col == colPk {
				exist = true
			}
		}
		return !exist
	}

	for _, index := range visitor.HashIndexList {
		notInPk := false
		for idx, col := range index.Cols {
			if funcNotInIndex(col, visitor.HashIndexPk) {
				index.KeyTypesStripPk += col + " " + index.Types[idx] + ","
				index.KeysStripPk = append(index.KeysStripPk, col)
				notInPk = true
			} else {
			}
		}
		if notInPk {
			if len(index.Cols) == 1 {
				for i := range visitor.ModifyColList {
					if visitor.ModifyColList[i] == index.Cols[0] {
						visitor.ModifyColList = append(visitor.ModifyColList[:i], visitor.ModifyColList[i+1:]...)
						break
					}
				}
			}
			visitor.ModifyHashIndexList = append(visitor.ModifyHashIndexList, index)
		}
	}
	if visitor.HashIndexPk != nil {
		visitor.HashIndexList = append([]*Index{visitor.HashIndexPk}, visitor.HashIndexList...)
	}

	for _, index := range visitor.HashIndexList {
		for _, otherIndex := range visitor.HashIndexList {
			for _, col := range index.KeysStripPk {
				if !funcNotInIndex(col, otherIndex) {
					index.EffectIndex = append(index.EffectIndex, otherIndex)
					break
				}
			}
		}
	}
	for _, col := range visitor.ModifyColList {
		for _, otherIndex := range visitor.HashIndexList {
			if !funcNotInIndex(col, otherIndex) {
				visitor.ColEffectHashMap[col] = append(visitor.ColEffectHashMap[col], otherIndex)
			}
		}
	}

	//// 主键强制放到第一个
	//for _, v := range indexDict {
	//	if v.Pk && v.Unique {
	//		visitor.HashIndexList = append(visitor.HashIndexList, v)
	//	}
	//}
	//for _, v := range indexDict {
	//	if !(v.Pk && v.Unique) {
	//		visitor.HashIndexList = append(visitor.HashIndexList, v)
	//	}
	//}

	// 查找是否存在Unload key索引
	if argsInfo.Unload {
		for _, keyName := range keyNameList {
			index := indexDict[keyName]
			// 必须存在数据库索引, 不然导入数据很慢
			if len(index.Cols) == 1 && index.Cols[0] == argsInfo.UnloadKey {
				if visitor.HashIndexUnload != nil {
					panic("repeated unload key. " + tableName + index.String())
				}
				visitor.HashIndexUnload = index
			} else {
				// 暂不支持联合键, 用作导出
			}
		}
	}
	if argsInfo.Unload && ValidWarningFlag(argsInfo.WarningFlag, EWarningFlagUnloadFunction) && visitor.HashIndexUnload == nil {
		fmt.Println("warring: cannot be generated method load. " + tableName + argsInfo.UnloadKey)
		//for _, v := range unloadWarringIndexList {
		//	fmt.Println("warring: cannot be generated method load. " + tableName + v.String())
		//}
	}
	//fmt.Println(visitor.HashIndexUnload)
	//	Uid
	//	int32
	//xorm:"pk" hash:"group=1;unique=0" hash:"group=2;unique=1"
	//	Id
	//	int32
	//xorm:"pk" hash:"group=2;unique=1"

	//for _, field := range GetStructBuildOutTypeFields(visitor.DataStructType) {
	//	block := GetFiledDeepCopyString(field)
	//	fmt.Println("##########",block)
	//}

	return
}

func FindPackageName(pkg string) string {
	pos := strings.LastIndex(pkg, "/")
	if pos != -1 {
		return strings.Trim(pkg[pos+1:], `"`)
	} else {
		// 去除字符串里面的双引号
		return strings.Trim(pkg, `"`)
	}
}

func (v *MyVisitor) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		v.Depth -= 1
		return nil
	}
	ts, ok := n.(*ast.TypeSpec)
	if ok && ts.Name.Name == v.DataName {
		v.DataTypeSpec = ts
		if structType, ok := ts.Type.(*ast.StructType); ok {
			v.DataStructType = structType
		}
	}

	// 获取node导入的包
	switch nodeType := n.(type) {
	case *ast.File:
		for _, spec := range nodeType.Imports {
			v.DSImportsPackage[FindPackageName(spec.Path.Value)] = spec.Path.Value
		}

	case *ast.Ident:
	case *ast.BasicLit:
	}

	//fmt.Printf("%s%T: %s\n", strings.Repeat("\t", int(v.Depth)), n, s)
	v.Depth += 1
	return v
}
