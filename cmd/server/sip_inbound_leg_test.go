package main

import (
	"sync"
	"testing"
)

// Fim da chamada WhatsApp enquanto o SIP ainda toca: só CANCEL, nunca BYE.
func TestInboundLegCancelBeforeAnswer(t *testing.T) {
	l := newInboundLeg()
	if got := l.end(); got != legActionCancel {
		t.Fatalf("end() while ringing = %v, want legActionCancel", got)
	}
	// um 2xx que chegue depois (cruzou com o CANCEL) não pode reativar o leg:
	// o chamador encerra com BYE.
	if l.markAnswered() {
		t.Fatal("markAnswered() after end() must return false")
	}
	if got := l.end(); got != legActionNone {
		t.Fatalf("second end() = %v, want legActionNone", got)
	}
}

// Fim da chamada WhatsApp depois do 2xx + ACK: só BYE, uma única vez.
func TestInboundLegByeAfterAnswer(t *testing.T) {
	l := newInboundLeg()
	if !l.markAnswered() {
		t.Fatal("markAnswered() while ringing must return true")
	}
	if got := l.end(); got != legActionBye {
		t.Fatalf("end() after answer = %v, want legActionBye", got)
	}
	if got := l.end(); got != legActionNone {
		t.Fatalf("second end() = %v, want legActionNone", got)
	}
}

// INVITE terminou sem 2xx (recusa/timeout/CANCEL): nada mais a enviar.
func TestInboundLegNoSignalAfterFailure(t *testing.T) {
	l := newInboundLeg()
	l.markEnded()
	if got := l.end(); got != legActionNone {
		t.Fatalf("end() after failed INVITE = %v, want legActionNone", got)
	}
	if l.markAnswered() {
		t.Fatal("markAnswered() after failure must return false")
	}
}

// O softphone desligou (BYE recebido): não responder com outro BYE.
func TestInboundLegNoByeAfterRemoteBye(t *testing.T) {
	l := newInboundLeg()
	if !l.markAnswered() {
		t.Fatal("markAnswered() while ringing must return true")
	}
	l.markEnded()
	if got := l.end(); got != legActionNone {
		t.Fatalf("end() after remote BYE = %v, want legActionNone", got)
	}
}

// Corrida entre o fim da chamada WhatsApp e o 2xx do PBX: exatamente um lado
// encerra o leg, com o sinal certo (CANCEL antes do 2xx, BYE depois).
func TestInboundLegRaceEndVsAnswer(t *testing.T) {
	for i := 0; i < 2000; i++ {
		l := newInboundLeg()
		start := make(chan struct{})
		var wg sync.WaitGroup
		var act inboundLegAction
		var answered bool
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			act = l.end()
		}()
		go func() {
			defer wg.Done()
			<-start
			answered = l.markAnswered()
		}()
		close(start)
		wg.Wait()

		switch {
		case answered && act == legActionBye:
			// 2xx primeiro: o fim da chamada manda BYE.
		case !answered && act == legActionCancel:
			// fim primeiro: CANCEL; quem recebeu o 2xx manda o BYE.
		default:
			t.Fatalf("iteration %d: inconsistent outcome answered=%v act=%v", i, answered, act)
		}
	}
}

// Vários fins concorrentes (ex.: OnStateChange e OnEnded do CallManager) depois
// do 2xx: só um BYE.
func TestInboundLegConcurrentEndSingleBye(t *testing.T) {
	l := newInboundLeg()
	if !l.markAnswered() {
		t.Fatal("markAnswered() while ringing must return true")
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		byes int
	)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.end() == legActionBye {
				mu.Lock()
				byes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if byes != 1 {
		t.Fatalf("got %d BYE actions, want exactly 1", byes)
	}
}

// Corrida entre o fim da chamada e o registro do diálogo (setDialog/dialogInfo)
// não pode ser um data race (go test -race).
func TestInboundLegDialogInfoConcurrent(t *testing.T) {
	l := newInboundLeg()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		l.setDialog(nil, "call-id-1")
	}()
	go func() {
		defer wg.Done()
		_ = l.end()
		_, _ = l.dialogInfo()
	}()
	wg.Wait()
	if _, id := l.dialogInfo(); id != "call-id-1" {
		t.Fatalf("sipCallID = %q, want call-id-1", id)
	}
}
