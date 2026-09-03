// boardinfo_test.go — BoardInfoFor (#477 P2): lectura del último board info
// cacheado (push del agente o sondeo SSH).
package adapters

import "testing"

func TestBoardInfoFor(t *testing.T) {
	l := &Live{boardCache: map[string]*BoardInfo{}}

	if bi := l.BoardInfoFor("desconocido"); bi != nil {
		t.Fatalf("router desconocido debe devolver nil, got %+v", bi)
	}

	b := &BoardInfo{Model: "Redmi AX6", BoardName: "redmi,ax6"}
	b.Release.Version = "25.12.5"
	b.Release.Target = "qualcommax/ipq807x"
	l.boardCache["rt4"] = b

	got := l.BoardInfoFor("rt4")
	if got == nil {
		t.Fatal("BoardInfoFor(rt4) = nil, esperaba el board cacheado")
	}
	if got.BoardName != "redmi,ax6" || got.Release.Target != "qualcommax/ipq807x" || got.Release.Version != "25.12.5" {
		t.Fatalf("board corrupto: %+v", got)
	}
}
