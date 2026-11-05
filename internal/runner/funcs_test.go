package runner

import (
	"strings"
	"testing"
)

// Source: knowledge/decisions/profile-v2.md, "Targets": a function target
// path#Name resolves to the function's line range with a cheap per-language
// parser, and "a function" includes Type.Method.

const goSrc = `package p

type T struct{}

func Plain() int {
	return 1
}

func (t *T) Method() {
	_ = Plain()
}

func (T) Other() {}
`

func TestFindFuncGo(t *testing.T) {
	cases := map[string][2]int{"Plain": {5, 7}, "Method": {9, 11}, "T.Method": {9, 11}, "T.Other": {13, 13}}
	for name, want := range cases {
		s, e, err := FindFunc("p.go", []byte(goSrc), name)
		if err != nil || s != want[0] || e != want[1] {
			t.Errorf("%s: got %d-%d %v, want %d-%d", name, s, e, err, want[0], want[1])
		}
	}
	if _, _, err := FindFunc("p.go", []byte(goSrc), "U.Method"); err == nil {
		t.Error("U.Method: want an error, the receiver is T")
	}
}

const csSrc = `namespace Api;

public class Users
{
    public User GetUser(int id)
    {
        var s = "}{";  // braces in a string
        /* } */
        return repo.GetUser(id);
    }

    public int Count() => repo
        .Count();

    public abstract void Save(User u);

    public Task<User> Load<T>(int id) { return Find(id); }
}
`

func TestFindFuncBraces(t *testing.T) {
	cases := map[string][2]int{
		"GetUser":       {5, 10}, // the call repo.GetUser( on line 9 is not a declaration
		"Users.GetUser": {5, 10},
		"Count":         {12, 13},
		"Load":          {17, 17},
	}
	for name, want := range cases {
		s, e, err := FindFunc("Users.cs", []byte(csSrc), name)
		if err != nil || s != want[0] || e != want[1] {
			t.Errorf("%s: got %d-%d %v, want %d-%d", name, s, e, err, want[0], want[1])
		}
	}
	for _, name := range []string{"Save", "Find", "Missing"} {
		if _, _, err := FindFunc("Users.cs", []byte(csSrc), name); err == nil {
			t.Errorf("%s: want an error: abstract, a call only, or absent", name)
		}
	}
}

func TestFindFuncAmbiguous(t *testing.T) {
	src := "class A {\n  void F(int a) {\n  }\n  void F(string s) {\n  }\n}\n"
	_, _, err := FindFunc("A.java", []byte(src), "F")
	if err == nil || !strings.Contains(err.Error(), "2 functions named F (lines 2, 4)") {
		t.Fatalf("err = %v", err)
	}
}

func TestFindFuncPython(t *testing.T) {
	src := "def a():\n    x = 1\n\n    # note\n    return x\n\nclass C:\n    async def b(self):\n        pass\n    def c(self):\n        pass\n"
	cases := map[string][2]int{"a": {1, 5}, "b": {8, 9}, "C.c": {10, 11}}
	for name, want := range cases {
		s, e, err := FindFunc("m.py", []byte(src), name)
		if err != nil || s != want[0] || e != want[1] {
			t.Errorf("%s: got %d-%d %v, want %d-%d", name, s, e, err, want[0], want[1])
		}
	}
}
