# PiqLink Direct Transfer per Linux

**File da un dispositivo all'altro, direttamente.** Nessun server per i file, nessuna registrazione, nessun limite di dimensione.

PiqLink trasferisce file e cartelle tra computer e telefoni con un codice breve (o il suo QR). È compatibile con PiqLink 2.5 per Windows e Android.

Un progetto [BrunoSoft](https://www.brunosoft.eu) · download: [www.brunosoft.eu](https://www.brunosoft.eu) · versione web: [www.piqlink.com](https://www.piqlink.com)

## Uso

Un solo eseguibile, senza dipendenze:

```sh
./piqlink                          # apre la finestra di PiqLink
./piqlink ricevi                   # mostra un codice (e il QR) da dare a chi invia
./piqlink ricevi CODICE            # riceve dal codice mostrato da chi invia
./piqlink invia FILE...            # mostra un codice da dare a chi riceve
./piqlink invia -c CODICE FILE...  # invia al codice mostrato da chi riceve
./piqlink --help
```

In inglese: `send`, `receive`, `--code`, `--dir`, `--yes`, `--assisted`.

**La finestra.** È una pagina servita solo su `127.0.0.1`, con un token casuale, e mostrata dal browser: come finestra a sé se c'è un browser della famiglia Chromium, altrimenti in una scheda. Il motore gira nel programma, in modo nativo. Per scegliere i file usa zenity o kdialog, se presenti.

## Compilare

Serve Go (vedi `go.mod`):

```sh
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o piqlink ./cmd/piqlinux
CGO_ENABLED=0 GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o piqlink-arm64 ./cmd/piqlinux   # Raspberry Pi
go test ./core
```

## Come funziona

| Parte | File |
|---|---|
| Codici (Crockford base32, 6/8/12/14 caratteri), QR | `core/code.go`, `core/qr.go` |
| Autenticazione con il codice: SPAKE2 (RFC 3526 MODP), HKDF | `core/pake.go` |
| Canale cifrato: AES-256-GCM a frame | `core/channel.go` |
| Protocollo di trasferimento, ripresa, avanzamento confermato | `core/transfer.go`, `core/store.go`, `core/collect.go` |
| Stessa rete e via Internet (UPnP) | `core/session.go`, `core/upnp.go`, `core/netinfo.go` |
| Collegamento assistito: STUN, hole punching UDP, codice di risposta | `core/p2p.go` |
| Trasporto affidabile su UDP (controllo di congestione tipo BBR) | `core/rudp.go` |
| Finestra (server locale + pagina) e terminale | `cmd/piqlinux/` |

- **Codice.** Il codice indica dove trovare il dispositivo e contiene un segreto. Entrambi i dispositivi lo usano per una stretta di mano SPAKE2. Chi non conosce il codice non può collegarsi, e chi intercetta il traffico non può né leggerlo né indovinare il codice. Dopo 3 tentativi con un codice sbagliato la ricezione si blocca.
- **Collegamento assistito.** Si usa solo con il consenso esplicito dell'utente. I server STUN pubblici (Google, Cloudflare) ricevono solo l'indirizzo IP. I file viaggiano sempre direttamente tra i due dispositivi.
- **Firewall.** Porte TCP di ricezione: 47760–47763 (`sudo ufw allow 47760:47763/tcp`).

## Lingue

Italiano, inglese, spagnolo e cinese.

---

## English

PiqLink transfers files and folders directly between computers and phones with a short code or QR: no server in between, no sign-up, no size limits. It is compatible with PiqLink 2.5 for Windows and Android.

Build with `CGO_ENABLED=0 go build -o piqlink ./cmd/piqlinux`. Run `./piqlink` for the window (served on 127.0.0.1 only and shown by the browser), or `./piqlink receive` / `./piqlink send FILE...` in a terminal.

© 2026 Romeo Nazzari — BrunoSoft · [www.brunosoft.eu](https://www.brunosoft.eu)
