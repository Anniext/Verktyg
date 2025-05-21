//go:generate go install
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path"
	tpl "persist/template"
	"persist/util"
	"strings"
	"sync"
	"text/template"

	"github.com/Masterminds/sprig/v3"
)

func GenPersistMethod(wg *sync.WaitGroup, srcDir, goFile, dstDir, goPackage, tableName string, argsInfo *util.ArgsInfo) {
	defer wg.Done()
	tt := template.Must(template.New("Main").Funcs(sprig.TxtFuncMap()).Parse(tpl.Main))
	//fmt.Print(tableName, " ,")
	genFileName := "001_" + strings.ToLower(tableName) + "_persist.go"

	goFilePathAbs := path.Join(srcDir, goFile)
	visitor := util.ParseTable(goFilePathAbs, tableName, argsInfo)
	visitor.PackageName = goPackage

	//fmt.Println(visitor)

	// 处理格式并调整所提供文件的导入
	var fileBuf = &bytes.Buffer{}
	filePath := path.Join(dstDir, genFileName)
	err := tt.Execute(fileBuf, visitor)
	if err != nil {
		panic(err.Error())
	}
	out := fileBuf.Bytes()
	//不能并发执行
	//out, err := imports.Process(filePath, fileBuf.Bytes(), nil)
	//if err != nil {
	//	panic(err.Error())
	//}
	err = os.WriteFile(filePath, out, 0666)
	if err != nil {
		panic(err.Error())
	}

	err = exec.Command("goimports", "-w", filePath).Run()
	if err != nil {
		fmt.Println(filePath)
		fmt.Println(err)
		log.Println(filePath)
		log.Println(err)
		panic(err.Error())
	}
	var syncMapCmd string
	var dataType string
	var dataTypeFullPath string
	if visitor.ArgsInfo.OptimizeFlagUsePoolAndDisableDeleteUnload() {
		syncMapCmd = "rwmap"
		dataType = "int"
		dataTypeFullPath = dataType
	} else {
		syncMapCmd = "syncmap"
		dataType = "*" + visitor.DataName
		dataTypeFullPath = "*" + argsInfo.PersistPkgName + "." + visitor.DataName
	}

	genPersistSet := false
	// 生成索引依赖的sync.Map
	if visitor.HashIndexUnload != nil {
		name := visitor.DataName + "MapUnload"
		filePath := path.Join(dstDir, "001_"+strings.ToLower(visitor.DataName)+"_map_unload.go")
		cmd := exec.Command(syncMapCmd, "-name="+name, "-pkg="+visitor.PackageName, "-o="+filePath, "map["+visitor.HashIndexUnload.Types[0]+"]*int32")
		//fmt.Println("syncMapCmd 1", "map["+visitor.HashIndexUnload.Types[0]+"]*int32")
		out, err = cmd.CombinedOutput()
		if err != nil {
			log.Panic("exec.syncmap gen persist set failed with", err.Error(), string(out))
		}
	}
	for _, v := range visitor.HashIndexList {
		if !v.Unique && !visitor.ArgsInfo.OptimizeFlagUsePoolAndDisableDeleteUnload() {
			if !genPersistSet {
				name := visitor.DataName + "Set"
				filePath := path.Join(dstDir, "001_"+strings.ToLower(visitor.DataName)+"_set.go")
				cmd := exec.Command(syncMapCmd, "-name="+name, "-pkg="+visitor.PackageName, "-o="+filePath, "map["+dataTypeFullPath+"]bool")
				//fmt.Println("syncMapCmd 2", "map["+dataTypeFullPath+"]bool")
				out, err = cmd.CombinedOutput()
				if err != nil {
					log.Panic("exec.syncmap gen persist set failed with", err.Error(), string(out))
				}
				genPersistSet = true
			}
		}
		if true || len(v.Cols) > 1 {
			if v.Unique {
				name := visitor.DataName + "Hash" + v.Keys
				filePath := path.Join(dstDir, "001_"+strings.ToLower(name)+".go")
				cmd := exec.Command(syncMapCmd, "-name="+name, "-pkg="+visitor.PackageName, "-o="+filePath, "map["+visitor.DataName+"KeyTypeHash"+v.Keys+"]"+dataTypeFullPath)
				//fmt.Println("syncMapCmd 3", "map["+visitor.DataName+"KeyTypeHash"+v.Keys+"]"+dataTypeFullPath)
				out, err = cmd.CombinedOutput()
				if err != nil {
					log.Panic("exec.syncmap gen"+name+" failed with", err.Error(), string(out))
				}
			} else {
				name := visitor.DataName + "Hash" + v.Keys
				filePath := path.Join(dstDir, "001_"+strings.ToLower(name)+".go")
				var cmd *exec.Cmd
				if visitor.ArgsInfo.OptimizeFlagUsePoolAndDisableDeleteUnload() {
					cmd = exec.Command(syncMapCmd, "-name="+name, "-pkg="+visitor.PackageName, "-o="+filePath, "map["+visitor.DataName+"KeyTypeHash"+v.Keys+"]"+dataTypeFullPath)
					//fmt.Println("syncMapCmd 4", "map["+visitor.DataName+"KeyTypeHash"+v.Keys+"]"+dataTypeFullPath)
				} else {
					cmd = exec.Command(syncMapCmd, "-name="+name, "-pkg="+visitor.PackageName, "-o="+filePath, "map["+visitor.DataName+"KeyTypeHash"+v.Keys+"]*"+visitor.DataName+"Set")
					//fmt.Println("syncMapCmd 5", "map["+visitor.DataName+"KeyTypeHash"+v.Keys+"]*"+visitor.DataName+"Set")
				}
				out, err = cmd.CombinedOutput()
				if err != nil {
					log.Panic("exec.syncmap gen"+name+" failed with", err.Error(), string(out))
				}
			}
		} else {
			if v.Unique {
				name := visitor.DataName + "Hash" + v.Keys
				filePath := path.Join(dstDir, "001_"+strings.ToLower(name)+".go")
				cmd := exec.Command(syncMapCmd, "-name="+name, "-pkg="+visitor.PackageName, "-o="+filePath, "map["+v.Types[0]+"]"+dataTypeFullPath)
				//fmt.Println("syncMapCmd 6", "map["+v.Types[0]+"]"+dataTypeFullPath)
				out, err = cmd.CombinedOutput()
				if err != nil {
					log.Panic("exec.syncmap gen"+name+" failed with", err.Error(), string(out))
				}
			} else {
				name := visitor.DataName + "Hash" + v.Keys
				filePath := path.Join(dstDir, "001_"+strings.ToLower(name)+".go")
				var cmd *exec.Cmd
				if visitor.ArgsInfo.OptimizeFlagUsePoolAndDisableDeleteUnload() {
					cmd = exec.Command(syncMapCmd, "-name="+name, "-pkg="+visitor.PackageName, "-o="+filePath, "map["+v.Types[0]+"]"+dataTypeFullPath)
					//fmt.Println("syncMapCmd 7", "map["+v.Types[0]+"]"+dataTypeFullPath)
				} else {
					cmd = exec.Command(syncMapCmd, "-name="+name, "-pkg="+visitor.PackageName, "-o="+filePath, "map["+v.Types[0]+"]*"+visitor.DataName+"Set")
					//fmt.Println("syncMapCmd 8", "map["+v.Types[0]+"]*"+visitor.DataName+"Set")
				}
				out, err = cmd.CombinedOutput()
				if err != nil {
					log.Panic("exec.syncmap gen"+name+" failed with", err.Error(), string(out))
				}
			}
		}
	}
}

var save = flag.Bool("save", true, "save data to mysql")
var srcDir = flag.String("src", "", "persist struct path")
var dstDir = flag.String("dst", "", "generate file path")
var fileName = flag.String("fileName", "", "persist file name")
var pkgName = flag.String("pkgName", "", "generate package name")
var unloadKey = flag.String("unloadKey", "Uid", "unload key name")
var unload = flag.Bool("unload", false, "can be unloaded")
var warningFlag = flag.Int64("warningFlag", math.MaxInt32, "warning flag")
var maxInsertRows = flag.Int64("maxInsertRows", 100, "max rows")
var queueThreshold = flag.Int64("queueThreshold", 10000, "sync queue threshold")
var queueEmptySleepTime = flag.Int64("queueEmptySleepTime", 100, "sleep time millisecond")
var NeedChangeDataBase = flag.Bool("separate", false, "Persist need separate database")
var rbtreeCap = flag.Int64("rbtreeCap", math.MaxInt32, "rbtree cap")
var SwitchTable = flag.String("switch", "", "switch table time interval") // day week month // 默认按一天分
var defaultOptimizeFlag int64 = (0 & util.EOptimizeFlagIndexMutex) |
	util.EOptimizeFlagSQLMerge |
	util.EOptimizeFlagGenerateCode |
	util.EOptimizeFlagSQLInsertMerge |
	util.EOptimizeFlagTraceSwitch |
	(0 & util.EOptimizeFlagUsePoolAndDisableDeleteUnload) |
	util.EOptimizeFlagMax
var optimizeFlag = flag.Int64("optimizeFlag", defaultOptimizeFlag, "optimize flag")

func findFirstFromRight(s, src string) int {
	if src == "" {
		return 0 // 如果src是空字符串，则认为它在任何位置都是匹配的
	}

	// 计算src的长度，用于后续比较
	srcLen := len(src)

	// 从s的末尾开始遍历
	for i := len(s) - srcLen; i >= 0; i-- {
		// 检查从i开始的后缀是否与src相等
		if s[i:i+srcLen] == src {
			return i // 返回src开始的位置
		}
	}

	// 如果没有找到，返回-1
	return -1
}

func getModuleFromGoMod() (string, error) {
	goModFile := "go.mod"

	file, err := os.Open(goModFile)
	if err != nil {
		return "", fmt.Errorf("failed to open go.mod: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				return parts[1], nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error while reading go.mod: %w", err)
	}

	return "", fmt.Errorf("module declaration not found in go.mod")
}

func main() {
	var err error
	var dir, goFile, goPackage, tableName string
	var persistPkgPath, persistPkgName string

	dir, err = os.Getwd()
	if err != nil {
		fmt.Printf("Could not GetWd(): %s (skip)\n", err)
		return
	}

	flag.Parse()

	goFile = os.Getenv("GOFILE")
	goPackage = os.Getenv("GOPACKAGE")

	modulePath, err := getModuleFromGoMod()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if *srcDir == "" {
		*srcDir = dir
	} else {
		persistPkgPath = modulePath + "/" + *srcDir
		persistPkgName = persistPkgPath[strings.LastIndex(persistPkgPath, "/")+1:]
		if !path.IsAbs(*srcDir) {
			*srcDir = path.Join(dir, *srcDir)
		}
	}

	if *dstDir == "" {
		*dstDir = dir
	} else {
		if !path.IsAbs(*dstDir) {
			*dstDir = path.Join(dir, *dstDir)
		}
	}

	if *fileName == "" {
		*fileName = goFile
	}
	if *pkgName == "" {
		*pkgName = goPackage
	}
	if *SwitchTable != "day" && *SwitchTable != "week" && *SwitchTable != "month" {
		*SwitchTable = ""
	} else {
		if *unload {
			panic("switch table can not unload")
		}
	}
	bombDir := "./" + strings.Replace(strings.Replace(strings.Replace(*dstDir, "\\", "_", -1), ":", "", -1), "/", "_", -1)

	srcBegin := findFirstFromRight(bombDir, "src")
	if srcBegin == -1 {
		srcBegin = 0
	}
	bombDir = "_" + bombDir[srcBegin:]

	argsInfo := util.ArgsInfo{
		Save:                *save,
		PersistPkgPath:      persistPkgPath,
		PersistPkgName:      persistPkgName,
		UnloadKey:           *unloadKey,
		Unload:              *unload,
		WarningFlag:         *warningFlag,
		BombDir:             bombDir,
		OptimizeFlag:        *optimizeFlag,
		MaxInsertRows:       *maxInsertRows,
		QueueThreshold:      *queueThreshold,
		QueueEmptySleepTime: *queueEmptySleepTime,
		FileName:            *fileName,
		NeedSeparate:        *NeedChangeDataBase,
		SwitchTable:         *SwitchTable,
		RBTreeCap:           int32(*rbtreeCap),
	}

	wg := &sync.WaitGroup{}
	for _, tableName = range flag.Args() {
		wg.Add(1)
		// 生成persist类
		go GenPersistMethod(wg, *srcDir, *fileName, *dstDir, *pkgName, tableName, &argsInfo)
	}
	wg.Wait()
	//fmt.Println()
}
