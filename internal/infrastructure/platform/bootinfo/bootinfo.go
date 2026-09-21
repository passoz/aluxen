// Package bootinfo registra o instante em que o processo começou a executar.
//
// É a fonte única de `StartedAt`/`Uptime` para o health, o `/api/info` e a
// identificação do build: quem apresentar o tempo de vida do processo lê daqui,
// sem duplicar o relógio de boot em cada pacote.
package bootinfo

import "time"

// StartedAt é o instante do boot do processo, fixado na inicialização.
var StartedAt = time.Now()
