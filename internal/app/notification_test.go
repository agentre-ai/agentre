package app

import "testing"

func TestSessionIDFromUserInfo(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]interface{}
		want int64
	}{
		{"float64(JSON 往返)", map[string]interface{}{"sessionID": float64(42)}, 42},
		{"int64", map[string]interface{}{"sessionID": int64(7)}, 7},
		{"int", map[string]interface{}{"sessionID": int(5)}, 5},
		{"string 数字", map[string]interface{}{"sessionID": "13"}, 13},
		{"缺失", map[string]interface{}{"other": 1}, 0},
		{"nil map", nil, 0},
		{"非法字符串", map[string]interface{}{"sessionID": "abc"}, 0},
		{"非数值类型", map[string]interface{}{"sessionID": true}, 0},
	}
	for _, c := range cases {
		if got := sessionIDFromUserInfo(c.in); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}
