// PiqLink Direct Transfer for Linux (BrunoSoft).
//
//	piqlink                     the window (in the browser, engine native)
//	piqlink invia FILE...       send from the terminal (shows a code)
//	piqlink invia -c CODE FILE  send to the code shown by the receiver
//	piqlink ricevi              receive from the terminal (shows a code)
//	piqlink ricevi CODE         receive from the code shown by the sender
package main

import (
	"fmt"
	"os"
	"strings"

	"piqlink/core"
)

// Version of this build: 2.5, like the published Windows and Android apps.
const version = "2.5"

func usage() {
	fmt.Printf(`PiqLink Direct Transfer %s per Linux - BrunoSoft · www.brunosoft.eu

  piqlink                          %s
  piqlink invia FILE...            %s
  piqlink invia -c CODICE FILE...  %s
  piqlink ricevi                   %s
  piqlink ricevi CODICE            %s

  -i, --internet   %s
  -a, --assistito  %s
  -d, --cartella   %s
  -s, --si         %s
      --installa   %s
      --no-browser %s

  (send / receive / --code / --dir / --yes / --assisted / --install)
`, version,
		tr("apre la finestra di PiqLink", "opens the PiqLink window"),
		tr("invia: mostra un codice da dare a chi riceve", "send: shows a code to give to the receiver"),
		tr("invia al codice mostrato da chi riceve", "send to the code shown by the receiver"),
		tr("ricevi: mostra un codice da dare a chi invia", "receive: shows a code to give to the sender"),
		tr("ricevi dal codice mostrato da chi invia", "receive from the code shown by the sender"),
		tr("codice via Internet (12 caratteri con UPnP, altrimenti collegamento assistito)", "code over the Internet (12 characters with UPnP, otherwise assisted connection)"),
		tr("consente il collegamento assistito (server pubblici STUN di Google e Cloudflare)", "allows the assisted connection (public STUN servers of Google and Cloudflare)"),
		tr("cartella dove salvare i file ricevuti", "folder for the received files"),
		tr("ricevi senza chiedere conferma", "receive without asking"),
		tr("aggiunge PiqLink al menu delle applicazioni", "adds PiqLink to the applications menu"),
		tr("non apre il browser: stampa l'indirizzo della finestra", "does not open the browser: prints the window address"))
}

func main() {
	core.AppVersion = version
	set := loadSettings()
	core.SetLang(set.Lang)
	core.SetSTUNAllowed(set.Assisted)
	if v := os.Getenv("PIQ_STUN"); v != "" { // tests only
		core.STUNServers = strings.Split(v, ",")
	}
	icon, _ := uiFS.ReadFile("ui/img/symbol.png")

	args := os.Args[1:]
	if len(args) == 0 {
		exitOn(runGUI(set, nil, icon, false))
		return
	}
	switch args[0] {
	case "invia", "send":
		os.Exit(cliSend(set, args[1:]))
	case "ricevi", "receive":
		os.Exit(cliReceive(set, args[1:]))
	case "-h", "--help", "aiuto", "help":
		usage()
	case "-v", "--version", "--versione":
		fmt.Println("PiqLink " + version)
	case "--installa", "--install":
		if err := installMenu(icon); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		set.MenuAsked = true
		set.save()
		fmt.Println(tr("PiqLink è nel menu delle applicazioni.", "PiqLink is in the applications menu."))
	case "--no-browser":
		exitOn(runGUI(set, nil, icon, true))
	default:
		// files from the file manager ("Apri con PiqLink"): window with them ready to send
		var paths []string
		for _, a := range args {
			if _, err := os.Stat(a); err == nil {
				paths = append(paths, a)
			}
		}
		if len(paths) == 0 {
			usage()
			os.Exit(2)
		}
		exitOn(runGUI(set, paths, icon, false))
	}
}

func exitOn(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
