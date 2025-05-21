package test

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

type PlayerCache struct {
	Name      string `json:"name,omitempty"`      // 玩家名字
	Icon      string `json:"icon,omitempty"`      // 玩家头像
	TitleId   int32  `json:"titleId,omitempty"`   // 玩家称号
	HeadFrame int32  `json:"headFrame,omitempty"` // 玩家称号
	ZoneId    int32  `json:"zoneId,omitempty"`    // 玩家服务器
}

func (m *MapInt32PlayerCache) FromDB(data []byte) (err error) {
	if len(data) == 0 {
		return nil
	}
	err = m.UnmarshalJSON(data)
	return
}

func (m *MapInt32PlayerCache) ToDB() (data []byte, err error) {
	data, err = m.MarshalJSON()
	return
}

func (m *MapInt32PlayerCache) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	tmp := map[int32]PlayerCache{}
	m.Range(func(key int32, value *PlayerCache) bool {
		tmp[key] = *value
		return true
	})
	ret, err := json.Marshal(&tmp)
	if err != nil {
		return nil, err
	}
	return ret, nil

}

func (m *MapInt32PlayerCache) UnmarshalJSON(b []byte) error {
	if m == nil {
		return errors.New(" Unmarshal(non-pointer MapInt32PlayerCache)")
	}
	tmp := map[int32]*PlayerCache{}
	err := json.Unmarshal(b, &tmp)
	if err != nil {
		return err
	}
	for k, v := range tmp {
		m.Store(k, v)
	}
	return nil
}

func (m *MapInt32PlayerCache) String() string {
	if m == nil {
		return "{}"
	}
	builder := strings.Builder{}
	builder.WriteString("{")
	m.Range(func(key int32, value *PlayerCache) bool {
		builder.WriteString("{")
		builder.WriteString(strconv.FormatInt(int64(key), 10))
		builder.WriteString(":")
		data, err := json.Marshal(value)
		if err != nil {
			builder.Write(data)
		} else {
			builder.WriteString("null")
		}
		builder.WriteString("}")
		builder.WriteString(",")
		return true
	})
	builder.WriteString("}")
	return builder.String()
}
