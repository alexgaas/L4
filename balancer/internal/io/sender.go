package io

type Sender interface {
	InitState() error
	Enqueue([]RecvMmsgData, int, bool)
	Copy() Sender
}

type Duplicator struct {
	Senders []Sender
}

func (b *Duplicator) Enqueue(message []RecvMmsgData, cnt int, isIPv6 bool) {
	for _, sender := range b.Senders {
		sender.Enqueue(message, cnt, isIPv6)
	}
}

func (b *Duplicator) InitState() error {
	for _, sender := range b.Senders {
		if err := sender.InitState(); err != nil {
			return err
		}
	}

	return nil
}

func (b *Duplicator) Copy() Duplicator {
	senders := make([]Sender, 0)
	for _, sender := range b.Senders {
		senders = append(senders, sender.Copy())
	}

	return Duplicator{Senders: senders}
}
