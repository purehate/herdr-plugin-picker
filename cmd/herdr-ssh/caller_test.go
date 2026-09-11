package main

import (
	"reflect"
	"testing"
)

func TestCurrentCallerReadsEnv(t *testing.T) {
	t.Setenv("HERDR_PANE_ID", "w9:p1")
	t.Setenv("HERDR_TAB_ID", "w9:t1")
	t.Setenv("HERDR_WORKSPACE_ID", "w9")

	want := caller{PaneID: "w9:p1", TabID: "w9:t1", WorkspaceID: "w9"}
	if got := currentCaller(); got != want {
		t.Fatalf("currentCaller = %+v, want %+v", got, want)
	}
}

func TestCallerEnvCarriesOnlyPopulatedFields(t *testing.T) {
	got := callerEnv(caller{PaneID: "w5:pA", WorkspaceID: "w5"})
	want := map[string]string{
		callerPaneEnv:      "w5:pA",
		callerWorkspaceEnv: "w5",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("callerEnv = %#v, want %#v", got, want)
	}
	if _, ok := got[callerTabEnv]; ok {
		t.Fatal("callerEnv forwarded an empty tab id")
	}
}

func TestPickerCallerReadsInvocationScopedEnv(t *testing.T) {
	t.Setenv(callerPaneEnv, "w9:p7")
	t.Setenv(callerTabEnv, "w9:t2")
	t.Setenv(callerWorkspaceEnv, "w9")

	want := caller{PaneID: "w9:p7", TabID: "w9:t2", WorkspaceID: "w9"}
	if got := pickerCaller(); got != want {
		t.Fatalf("pickerCaller = %+v, want %+v", got, want)
	}
}
