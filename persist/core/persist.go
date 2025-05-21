package core

import (
	"errors"
	"fmt"
	"github.com/getsentry/sentry-go"
	_ "github.com/go-sql-driver/mysql"
	"sync"
	"time"
)

type PersistError string

func (e PersistError) Error() string { return string(e) }

const ELoadPollingTimeOut = 5
const EMarshalFlagPoint uint8 = 0b00000001
const EMarshalFlagBitSet uint8 = 0b10000000

const EPersistStateDisk = 0
const EPersistStateLoading = 1
const EPersistStateMemory = 2
const EPersistStatePrepareUnloading = 3
const EPersistStateUnloading = 4

const EPersistErrorEngineNil = PersistError("persist: engine is nil")           // 启动关闭错误: 数据库连接失败
const EPersistErrorTempFileExist = PersistError("persist: temp file exist")     // 启动关闭错误: 存在临时bomb文件
const EPersistErrorInvalidBombFile = PersistError("persist: invalid bomb file") // 启动关闭错误: 无效的bomb文件
const EPersistErrorUnknownError = PersistError("persist: unknown error")        // 导入导出错误: 未知错误, 可能是并发引起
const EPersistErrorIncorrectState = PersistError("persist: incorrect state")    // 导入导出错误: 重复全导入或正在全导出
const EPersistErrorUnloading = PersistError("persist: unloading state")         // 导入导出错误: 正在导出, 导出完成后方可导入
const EPersistErrorAlreadyLoadAll = PersistError("persist: already load all")   // 导入导出错误: 已经全导入不能再按照key操作
const EPersistErrorLoading = PersistError("persist: loading state")             // 导入导出错误: 正在导入, 导入完成后方可导出
const EPersistErrorAlreadyLoad = PersistError("persist: already load")          // 导入导出错误: 重复导入
const EPersistErrorAlreadyUnload = PersistError("persist: already unload")      // 导入导出错误: 重复导出
const EPersistErrorNil = PersistError("persist: nil")                           // 增删改查错误: 非法的内存地址或空指针
const EPersistErrorAlreadyExist = PersistError("persist: already exist")        // 增删改查错误: 对象已经存在
const EPersistErrorNotInMemory = PersistError("persist: not in memory")         // 增删改查错误: 数据不在内存中
const EPersistErrorOutOfDate = PersistError("persist: out of date")             // 增删改查错误: 数据过期, 应当重新查询

// IPersist 所有persist必须实现接口
type IPersist interface {
	Sync(wg *sync.WaitGroup) (err error)                       // 启动同步表结构
	Exit(wg *sync.WaitGroup)                                   // 退出
	Run() (err error)                                          // 启动
	Dead() bool                                                // 是否死亡
	PersistName() string                                       // 获取结构名
	RecoverBomb(bomb []byte) (err error)                       // 恢复数据通过 bomb数据
	SyncData(wg *sync.WaitGroup, sentryDebug bool) (err error) // 检查内存数据并同步到数据库
	RecoverTrace(trace [][]byte) (err error)                   // 恢复数据通过 trace数据
	StringToPersistSyncInterface(data string) interface{}      // string类型数据 转化成 PersistSync结构
	BytesToPersistInterface(data []byte) interface{}           // bytes类型数据 转化成 Persist结构
	PersistInterfaceToBytes(i interface{}) []byte              // Persist结构 转化成 bytes类型数据
	PersistInterfaceToPkStruct(i interface{}) interface{}      // Persist结构 转成 主键结构体interface
	LazyInit() (err error)                                     // 惰性创建注册初始化
	Segmentation(wg *sync.WaitGroup) (err error) // 切换表名并创建新表
}

// IPersistUser 用户相关persist必须实现接口
type IPersistUser interface {
	IPersist
	Load(Uid int32) (err error)                           // 导入用户UID的数据
	Unload(Uid int32) (err error)                         // 导出用户UID的数据
	SetLoadState2Memory(Uid int32)                        // 将用户UID的数据强制设置为导入到内存中的状态
	LoadState(Uid int32) int32                            // 查询用户UID的导入状态
	SyncUserData(Uid int32, sentryDebug bool) (err error) // 检查用户UID的内存数据并同步到数据库
	PersistUserNilObjInterface() interface{}              // 获取PersistUser对象的nil指针
	PersistUserNilObjInterfaceList() interface{}          // 获取PersistUser对象数组的nil指针
}

var gPersistMap = make(map[string]IPersist)
var gPersistUserMap = make(map[string]IPersistUser)
var gPersistMapLazy = make(map[string]IPersist)

// RegisterPersistLazy 惰性注册
func RegisterPersistLazy(name string, persist IPersist) {
	if _, ok := gPersistMapLazy[name]; ok {
		panic(errors.New("repeated register lazy persist " + name))
	}
	gPersistMapLazy[name] = persist
}

// RegisterPersist 注册
func RegisterPersist(name string, persist IPersist) {
	if _, ok := gPersistMap[name]; ok {
		panic(errors.New("repeated register persist " + name))
	}
	gPersistMap[name] = persist

	persistUser, ok := persist.(IPersistUser)
	if ok {
		if _, ok := gPersistUserMap[name]; ok {
			fmt.Printf("%#v\n", gPersistUserMap)
			panic(errors.New("repeated register persist user " + name))
		}
		gPersistUserMap[name] = persistUser
	} else {
	}
}

// ChangeRegister 非注册 更换已经注册的
func ChangeRegister(name string, persist IPersist) {
	if _, ok := gPersistMap[name]; ok {
		gPersistMap[name] = persist
	} else {

	}
}

func GetIPersistByName(name string) IPersist {
	persist, ok := gPersistMap[name]
	if ok {
		return persist
	} else {
		return nil
	}
}

// Load 按照用户uid导入
func Load(uid int32) (err error) {
	for _, persist := range gPersistUserMap {
		err = persist.Load(uid)
		if err != nil {
			return errors.New(persist.PersistName() + err.Error())
		}
	}
	return
}

// SetLoadState2Memory 确定数据一致性前提下，强制设置用户数据已导入
func SetLoadState2Memory(uid int32) {
	for _, persist := range gPersistUserMap {
		persist.SetLoadState2Memory(uid)
	}
	return
}

// Unload 按照用户uid导出
func Unload(uid int32) (err error) {
	for _, persist := range gPersistUserMap {
		err = persist.Unload(uid)
		if err != nil {
			return errors.New(persist.PersistName() + err.Error())
		}
	}
	return
}

// LoadState 所有用户数据导入状态
func LoadState(uid int32) (stateList []int32) {
	for _, persist := range gPersistUserMap {
		stateList = append(stateList, persist.LoadState(uid))
	}
	return
}

// SyncPersist 所有Persist同步结构
func SyncPersist() error {
	var persistNameList []string
	for name := range gPersistMapLazy {
		persistNameList = append(persistNameList, name)
	}
	for i := range persistNameList {
		err := gPersistMapLazy[persistNameList[i]].LazyInit()
		if err != nil {
			return err
		} else {
			delete(gPersistMapLazy, persistNameList[i])
		}
	}
	errMap := map[string]error{}

	var wg sync.WaitGroup
	for key := range gPersistMap {
		wg.Add(1)
		go func(key string) {
			err := gPersistMap[key].Sync(&wg)
			if err != nil {
				errMap[key] = err
				panic(errors.New(key + err.Error()))
			}
		}(key)
	}
	wg.Wait()

	for _, err := range errMap {
		if err != nil {
			return err
		}
	}
	return nil
}

// RunPersist 运行所有Persist
func RunPersist() error {
	for _, persist := range gPersistMap {
		err := persist.Run()
		if err != nil {
			return errors.New(persist.PersistName() + err.Error())
		}
	}
	return nil
}

// DeadPersist 是否存在异常状态Persist
func DeadPersist() bool {
	for _, persist := range gPersistMap {
		dead := persist.Dead()
		if dead {
			return true
		}
	}
	return false
}

// ExitPersist 退出所有Persist
func ExitPersist() {
	var wg sync.WaitGroup
	for key := range gPersistMap {
		wg.Add(1)
		go gPersistMap[key].Exit(&wg)
	}
	wg.Wait()
}

// SyncDataPersist 所有Persist, 不安全的方式强制同步数据, 调用后不允许再修改数据
func SyncDataPersist(sentryDebug bool) error {

	var wg sync.WaitGroup
	ch := make(chan error, len(gPersistMap))
	for key := range gPersistMap {
		wg.Add(1)
		go func(name string) {
			err := gPersistMap[name].SyncData(&wg, sentryDebug)
			if err != nil {
				ch <- errors.New(name + err.Error())
			}
		}(key)
	}

	wg.Wait()
	sentry.Flush(time.Second * 5)
	select {
	case err, ok := <-ch:
		if ok {
			return err
		} else {
			return nil
		}
	default:
		return nil
	}
}

// SyncUserDataPersist 用户相关Persist, 不安全的方式强制同步数据, 调用后不允许再修改数据
func SyncUserDataPersist(uid int32, sentryDebug bool) (err error) {
	for _, persist := range gPersistUserMap {
		err = persist.SyncUserData(uid, sentryDebug)
		if err != nil {
			return errors.New(persist.PersistName() + err.Error())
		}
	}
	sentry.Flush(time.Second * 5)
	return
}

// GetPersistList 注册的IPersist列表
func GetPersistList() (list []IPersist) {
	for _, persist := range gPersistMap {
		list = append(list, persist)
	}
	return
}

// GetPersistUserList 注册的IPersistUser列表
func GetPersistUserList() (list []IPersistUser) {
	for _, persist := range gPersistUserMap {
		list = append(list, persist)
	}
	return
}

// GetGPersistUserMap 获取 gPersistUserMap
func GetGPersistUserMap() map[string]IPersistUser {
	return gPersistUserMap
}

// SegmentationPersist 检查IPersist 配置切换写入表名
func SegmentationPersist() { // 定时任务调用 实现切表
	var wg sync.WaitGroup
	ch := make(chan error, len(gPersistMap))
	for key := range gPersistMap {
		wg.Add(1)
		go func(name string) {
			err := gPersistMap[name].Segmentation(&wg)
			if err != nil {
				ch <- errors.New(name + err.Error())
			}
		}(key)
	}
	wg.Wait()
}
