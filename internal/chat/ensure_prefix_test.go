package chat

import (
	"testing"
)

func TestEnsurePMToUSR_NoAt(t *testing.T) {
	m := &Manager{}
	got := m.ensurePMToUSR("请创建hello.go")
	want := "@USR 请创建hello.go"
	if got != want {
		t.Errorf("ensurePMToUSR() = %q, want %q", got, want)
	}
}

func TestEnsurePMToUSR_HasUSR(t *testing.T) {
	m := &Manager{}
	got := m.ensurePMToUSR("@USR 请创建hello.go")
	want := "@USR 请创建hello.go"
	if got != want {
		t.Errorf("ensurePMToUSR() = %q, want %q", got, want)
	}
}

func TestEnsurePMToUSR_DualAt_Space_Content(t *testing.T) {
	t.Run("re1 pattern", func(t *testing.T) {
		m := &Manager{}
		got := m.ensurePMToUSR("@USR @ 请创建hello.go")
		want := "@USR 请创建hello.go"
		if got != want {
			t.Errorf("ensurePMToUSR() = %q, want %q", got, want)
		}
	})
	t.Run("re2 pattern", func(t *testing.T) {
		m := &Manager{}
		got := m.ensurePMToUSR("@USR SE 请在工作目录下创建 hello.go")
		want := "@USR 请在工作目录下创建 hello.go"
		if got != want {
			t.Errorf("ensurePMToUSR() = %q, want %q", got, want)
		}
	})
	t.Run("re3 pattern", func(t *testing.T) {
		m := &Manager{}
		got := m.ensurePMToUSR("@USR @SE 请创建hello.go")
		want := "@USR 请创建hello.go"
		if got != want {
			t.Errorf("ensurePMToUSR() = %q, want %q", got, want)
		}
	})
}

func TestEnsurePMToUSR_SwappedRoles(t *testing.T) {
	m := &Manager{}
	got := m.ensurePMToUSR("@SE @USR 请创建")
	want := "@USR 请创建"
	if got != want {
		t.Errorf("ensurePMToUSR() = %q, want %q", got, want)
	}
}

func TestEnsurePMToUSR_WithTaskJSON(t *testing.T) {
	m := &Manager{}
	got := m.ensurePMToUSR("@USR @ 请创建 hello.go\n{\"current_task\":\"test\"}")
	want := "@USR 请创建 hello.go\n{\"current_task\":\"test\"}"
	if got != want {
		t.Errorf("ensurePMToUSR() = %q, want %q", got, want)
	}
}

func TestEnsurePMToUSR_Empty(t *testing.T) {
	m := &Manager{}
	got := m.ensurePMToUSR("")
	want := "@USR "
	if got != want {
		t.Errorf("ensurePMToUSR() = %q, want %q", got, want)
	}
}

func TestEnsurePMToUSR_OnlyAtUSR(t *testing.T) {
	m := &Manager{}
	got := m.ensurePMToUSR("@USR")
	want := "@USR"
	if got != want {
		t.Errorf("ensurePMToUSR() = %q, want %q", got, want)
	}
}

func TestEnsurePMToUSR_NormalMessage_Preserved(t *testing.T) {
	m := &Manager{}
	got := m.ensurePMToUSR("@USR 你好，任务完成了吗")
	want := "@USR 你好，任务完成了吗"
	if got != want {
		t.Errorf("ensurePMToUSR() = %q, want %q", got, want)
	}
}

func TestEnsurePMToSE_NoAt(t *testing.T) {
	m := &Manager{}
	got := m.ensurePMToSE("请创建hello.go")
	want := "@SE 请创建hello.go"
	if got != want {
		t.Errorf("ensurePMToSE() = %q, want %q", got, want)
	}
}

func TestEnsurePMToSE_HasSE(t *testing.T) {
	m := &Manager{}
	got := m.ensurePMToSE("@SE 请创建hello.go")
	want := "@SE 请创建hello.go"
	if got != want {
		t.Errorf("ensurePMToSE() = %q, want %q", got, want)
	}
}

func TestEnsurePMToSE_DualAt(t *testing.T) {
	m := &Manager{}
	got := m.ensurePMToSE("@SE @ 请创建hello.go")
	want := "@SE 请创建hello.go"
	if got != want {
		t.Errorf("ensurePMToSE() = %q, want %q", got, want)
	}
}

func TestEnsurePMToSE_FromUSR(t *testing.T) {
	m := &Manager{}
	got := m.ensurePMToSE("@USR @SE 请创建hello.go")
	want := "@SE 请创建hello.go"
	if got != want {
		t.Errorf("ensurePMToSE() = %q, want %q", got, want)
	}
}

func TestEnsurePMToSE_WithTaskJSON(t *testing.T) {
	m := &Manager{}
	got := m.ensurePMToSE("@SE @ 请创建 hello.go\n{\"current_task\":\"test\"}")
	want := "@SE 请创建 hello.go\n{\"current_task\":\"test\"}"
	if got != want {
		t.Errorf("ensurePMToSE() = %q, want %q", got, want)
	}
}

func TestEnsurePMToSE_NoLeadingAt(t *testing.T) {
	m := &Manager{}
	got := m.ensurePMToSE("创建hello.go文件")
	want := "@SE 创建hello.go文件"
	if got != want {
		t.Errorf("ensurePMToSE() = %q, want %q", got, want)
	}
}

func TestEnsurePMToSE_USR_withoutAt(t *testing.T) {
	m := &Manager{}
	got := m.ensurePMToSE("@USR SE 请在工作目录下创建")
	want := "@SE 请在工作目录下创建"
	if got != want {
		t.Errorf("ensurePMToSE() = %q, want %q", got, want)
	}
}
