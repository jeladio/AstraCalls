package main

import (
	"sync"

	"github.com/emiago/sipgo"
)

// inboundLegState é o estado do leg SIP de uma chamada WhatsApp->SIP (somos o UAC).
type inboundLegState int

const (
	legRinging  inboundLegState = iota // INVITE enviado, ainda sem 2xx
	legAnswered                        // 2xx recebido e ACK enviado: diálogo confirmado
	legEnded                           // encerrado (cancelado, recusado ou desligado)
)

// inboundLegAction diz ao gateway o que mandar ao PBX quando a chamada WhatsApp termina.
type inboundLegAction int

const (
	legActionNone   inboundLegAction = iota // nada a enviar (já encerrado)
	legActionCancel                         // ainda tocando: CANCEL (cancelando o contexto do WaitAnswer)
	legActionBye                            // diálogo confirmado: BYE
)

// inboundLeg serializa os eventos que chegam de goroutines diferentes (fim da
// chamada WhatsApp, resposta do PBX, BYE do softphone) e garante que o leg recebe
// no máximo UM entre CANCEL (antes do 2xx) e BYE (depois do 2xx). Antes o OnEnded
// mandava os dois juntos: o BYE "early" encerrava a transação do INVITE, o PBX
// respondia 481 ao CANCEL e o 487 ficava sem ACK (retransmitido pelo PBX).
type inboundLeg struct {
	mu        sync.Mutex
	state     inboundLegState
	dialog    *sipgo.DialogClientSession
	sipCallID string
}

func newInboundLeg() *inboundLeg {
	return &inboundLeg{state: legRinging}
}

// setDialog guarda o diálogo e o Call-ID SIP assim que o INVITE é enviado.
func (l *inboundLeg) setDialog(d *sipgo.DialogClientSession, sipCallID string) {
	l.mu.Lock()
	l.dialog = d
	l.sipCallID = sipCallID
	l.mu.Unlock()
}

func (l *inboundLeg) dialogInfo() (*sipgo.DialogClientSession, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dialog, l.sipCallID
}

// end é chamado quando a chamada WhatsApp termina. Devolve o que mandar ao PBX;
// chamadas repetidas devolvem legActionNone.
func (l *inboundLeg) end() inboundLegAction {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch l.state {
	case legRinging:
		l.state = legEnded
		return legActionCancel
	case legAnswered:
		l.state = legEnded
		return legActionBye
	}
	return legActionNone
}

// markAnswered é chamado depois do 2xx + ACK. false = a chamada WhatsApp já tinha
// terminado (o 2xx cruzou com o CANCEL) e o chamador precisa encerrar com BYE.
func (l *inboundLeg) markAnswered() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.state != legRinging {
		return false
	}
	l.state = legAnswered
	return true
}

// markEnded marca o leg como encerrado sem enviar nada: o INVITE terminou sem 2xx
// (recusa, timeout, cancelamento) ou o softphone já mandou BYE.
func (l *inboundLeg) markEnded() {
	l.mu.Lock()
	l.state = legEnded
	l.mu.Unlock()
}
