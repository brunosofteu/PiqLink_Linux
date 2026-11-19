// PiqLink 2.1 for Linux - BrunoSoft. The window: the engine runs in the
// piqlink program on this computer; this page only shows its state.
"use strict";

var TX = {
  it: {
    useAssist: "Usa il collegamento assistito", replyTitle: "Codice di risposta", replySub: "Collegamento assistito: l'altro dispositivo ti detta un codice di 10 caratteri. Scrivilo qui.", confirmReply: "Conferma", replyOk: "Codice di risposta accettato: collegamento in corso…", replyShowTitle: "Codice di risposta", replyShowSub: "Dettalo a chi ti ha dato il codice: lo scrive nel riquadro «Codice di risposta».", assistTitle: "Collegamento assistito", consentText: "Quando un router non accetta collegamenti da Internet (succede spesso con le reti mobili), PiqLink può chiedere a un server pubblico STUN di Google o Cloudflare come i due dispositivi si vedono da Internet. Poi i due dispositivi aprono tra loro un passaggio diretto.", consent1: "Il server riceve solo l'indirizzo IP: nessun file, nessun nome.", consent2: "I file viaggiano sempre direttamente tra i due dispositivi, cifrati.", consent3: "Puoi cambiare idea quando vuoi nelle Impostazioni.", notNow: "Non ora", allow: "Consenti", assistSet: "Collegamento assistito via Internet", assistSetSub: "Quando il router non apre porte, un server pubblico (STUN) aiuta a trovare la strada", stAssisting: "Collegamento assistito: ricerca dell'indirizzo pubblico…",
    send: "Invia", sendSub: "Scegli i file e inserisci il codice", recv: "Ricevi", recvSub: "Genera il codice o inserisci quello del mittente",
    settings: "Impostazioni", openDir: "Apri cartella", tagline: "File da un dispositivo all'altro, direttamente. Nessun server.",
    menuAsk: "Vuoi PiqLink nel menu delle applicazioni, con la sua icona?", noThanks: "No, grazie", add: "Aggiungi",
    dropTitle: "Trascina qui i file", dropSub: "oppure clicca per sceglierli", addFiles: "Aggiungi file", addDir: "Aggiungi cartella",
    codeFromRecv: "Codice del destinatario", codeFromRecvSub: "Chi riceve sceglie «Ricevi» e ti dà un codice di 6, 8, 12 o 14 caratteri.",
    sendGo: "Invia", orShow: "Oppure è il destinatario che scrive il codice:", genCode: "Genera tu il codice",
    lan: "Stessa rete", net: "Via Internet", haveCode: "Ho il codice del mittente", cancel: "Annulla",
    titleRecv: "Dai questo codice a chi invia", titleSend: "Dai questo codice a chi riceve",
    hRecv: "Ricevi", hSend: "Invia con il tuo codice",
    stStarting: "Preparazione del codice…", stWaiting: "In attesa dell'altro dispositivo…", stPeer: "Collegato con {0}…", stWaitAccept: "In attesa che {0} accetti i file…",
    expires: " · scade alle {0}",
    fwHint: "Se l'altro dispositivo non trova questo computer, controlla il firewall: per esempio sudo ufw allow 47760:47763/tcp",
    senderCode: "Codice del mittente", senderCodeSub: "Scrivi il codice mostrato dal dispositivo che invia (Invia › «Genera tu il codice»).",
    connect: "Collegati", back: "Indietro", connecting: "Collegamento…",
    askTitle: "{0} vuole inviarti", nFiles: "{0} file · {1}", reject: "Rifiuta", accept: "Accetta", askLeft: "Rispondi entro {0}",
    sendingTo: "Invio a {0}", receivingFrom: "Ricezione da {0}", direct: "Collegamento diretto e cifrato tra i due dispositivi", left: "mancano {0}",
    home: "Home", sentTo: "{0} file inviati a {1}.", recvFrom: "{0} file ricevuti da {1}.",
    devName: "Nome di questo computer", devNameSub: "Lo vede l'altro dispositivo", saveIn: "Salva i file ricevuti in", change: "Cambia",
    askBefore: "Chiedi prima di ricevere", askBeforeSub: "Mostra i file in arrivo e chiede se accettarli",
    language: "Lingua", menu: "Menu delle applicazioni", menuSub: "Aggiunge PiqLink con la sua icona", quit: "Esci da PiqLink",
    infoText: "Trasferimento diretto di file tra computer e telefoni, senza server e senza registrazione. Compatibile con PiqLink per Windows e Android.",
    close: "Chiudi", noFiles: "Scegli prima i file da inviare.", uploading: "Preparazione dei file… {0}",
    dirPrompt: "Cartella dove salvare i file ricevuti:", closed: "PiqLink è stato chiuso. Puoi chiudere questa finestra.", lost: "Collegamento con PiqLink perso. Riavvia PiqLink."
  },
  en: {
    useAssist: "Use the assisted connection", replyTitle: "Reply code", replySub: "Assisted connection: the other device dictates a 10-character code to you. Type it here.", confirmReply: "Confirm", replyOk: "Reply code accepted: connecting…", replyShowTitle: "Reply code", replyShowSub: "Dictate it to whoever gave you the code: they type it in the “Reply code” box.", assistTitle: "Assisted connection", consentText: "When a router does not accept connections from the Internet (common on mobile networks), PiqLink can ask a public STUN server from Google or Cloudflare how the two devices are seen from the Internet. Then the two devices open a direct path between them.", consent1: "The server only receives the IP address: no files, no names.", consent2: "Files always travel directly between the two devices, encrypted.", consent3: "You can change your mind at any time in Settings.", notNow: "Not now", allow: "Allow", assistSet: "Assisted connection over the Internet", assistSetSub: "When the router does not open ports, a public server (STUN) helps to find the way", stAssisting: "Assisted connection: finding the public address…",
    send: "Send", sendSub: "Choose files and enter the code", recv: "Receive", recvSub: "Generate a code or enter the sender's",
    settings: "Settings", openDir: "Open folder", tagline: "Files from one device to another, directly. No server.",
    menuAsk: "Do you want PiqLink in the applications menu, with its icon?", noThanks: "No, thanks", add: "Add",
    dropTitle: "Drop files here", dropSub: "or click to choose them", addFiles: "Add files", addDir: "Add folder",
    codeFromRecv: "Receiver's code", codeFromRecvSub: "The receiver chooses “Receive” and gives you a code of 6, 8, 12 or 14 characters.",
    sendGo: "Send", orShow: "Or the receiver types the code:", genCode: "Generate the code yourself",
    lan: "Same network", net: "Over the Internet", haveCode: "I have the sender's code", cancel: "Cancel",
    titleRecv: "Give this code to the sender", titleSend: "Give this code to the receiver",
    hRecv: "Receive", hSend: "Send with your code",
    stStarting: "Preparing the code…", stWaiting: "Waiting for the other device…", stPeer: "Connected to {0}…", stWaitAccept: "Waiting for {0} to accept the files…",
    expires: " · expires at {0}",
    fwHint: "If the other device cannot find this computer, check the firewall: for example sudo ufw allow 47760:47763/tcp",
    senderCode: "Sender's code", senderCodeSub: "Type the code shown by the sending device (Send › “Generate the code yourself”).",
    connect: "Connect", back: "Back", connecting: "Connecting…",
    askTitle: "{0} wants to send you", nFiles: "{0} files · {1}", reject: "Reject", accept: "Accept", askLeft: "Answer within {0}",
    sendingTo: "Sending to {0}", receivingFrom: "Receiving from {0}", direct: "Direct, encrypted connection between the two devices", left: "{0} left",
    home: "Home", sentTo: "{0} files sent to {1}.", recvFrom: "{0} files received from {1}.",
    devName: "Name of this computer", devNameSub: "The other device sees it", saveIn: "Save received files in", change: "Change",
    askBefore: "Ask before receiving", askBeforeSub: "Shows the incoming files and asks whether to accept them",
    language: "Language", menu: "Applications menu", menuSub: "Adds PiqLink with its icon", quit: "Quit PiqLink",
    infoText: "Direct file transfer between computers and phones, without servers and without sign-up. Compatible with PiqLink for Windows and Android.",
    close: "Close", noFiles: "Choose the files to send first.", uploading: "Preparing the files… {0}",
    dirPrompt: "Folder for received files:", closed: "PiqLink has been closed. You can close this window.", lost: "Connection with PiqLink lost. Restart PiqLink."
  },
  es: {
    useAssist: "Usar la conexión asistida", replyTitle: "Código de respuesta", replySub: "Conexión asistida: el otro dispositivo te dicta un código de 10 caracteres. Escríbelo aquí.", confirmReply: "Confirmar", replyOk: "Código de respuesta aceptado: conectando…", replyShowTitle: "Código de respuesta", replyShowSub: "Díctaselo a quien te dio el código: lo escribe en el recuadro «Código de respuesta».", assistTitle: "Conexión asistida", consentText: "Cuando un router no acepta conexiones desde Internet (es frecuente en las redes móviles), PiqLink puede preguntar a un servidor público STUN de Google o Cloudflare cómo se ven los dos dispositivos desde Internet. Después los dos dispositivos abren entre ellos un paso directo.", consent1: "El servidor solo recibe la dirección IP: ningún archivo, ningún nombre.", consent2: "Los archivos viajan siempre directamente entre los dos dispositivos, cifrados.", consent3: "Puedes cambiar de idea cuando quieras en Ajustes.", notNow: "Ahora no", allow: "Permitir", assistSet: "Conexión asistida por Internet", assistSetSub: "Cuando el router no abre puertos, un servidor público (STUN) ayuda a encontrar el camino", stAssisting: "Conexión asistida: buscando la dirección pública…",
    send: "Enviar", sendSub: "Elige los archivos e introduce el código", recv: "Recibir", recvSub: "Genera el código o introduce el del remitente",
    settings: "Ajustes", openDir: "Abrir carpeta", tagline: "Archivos de un dispositivo a otro, directamente. Sin servidor.",
    menuAsk: "¿Quieres PiqLink en el menú de aplicaciones, con su icono?", noThanks: "No, gracias", add: "Añadir",
    dropTitle: "Arrastra aquí los archivos", dropSub: "o haz clic para elegirlos", addFiles: "Añadir archivos", addDir: "Añadir carpeta",
    codeFromRecv: "Código del destinatario", codeFromRecvSub: "Quien recibe elige «Recibir» y te da un código de 6, 8, 12 o 14 caracteres.",
    sendGo: "Enviar", orShow: "O es el destinatario quien escribe el código:", genCode: "Genera tú el código",
    lan: "Misma red", net: "Por Internet", haveCode: "Tengo el código del remitente", cancel: "Cancelar",
    titleRecv: "Da este código a quien envía", titleSend: "Da este código a quien recibe",
    hRecv: "Recibir", hSend: "Enviar con tu código",
    stStarting: "Preparando el código…", stWaiting: "Esperando al otro dispositivo…", stPeer: "Conectado con {0}…", stWaitAccept: "Esperando a que {0} acepte los archivos…",
    expires: " · caduca a las {0}",
    fwHint: "Si el otro dispositivo no encuentra este ordenador, revisa el firewall: por ejemplo sudo ufw allow 47760:47763/tcp",
    senderCode: "Código del remitente", senderCodeSub: "Escribe el código que muestra el dispositivo que envía (Enviar › «Genera tú el código»).",
    connect: "Conectar", back: "Atrás", connecting: "Conectando…",
    askTitle: "{0} quiere enviarte", nFiles: "{0} archivos · {1}", reject: "Rechazar", accept: "Aceptar", askLeft: "Responde en {0}",
    sendingTo: "Enviando a {0}", receivingFrom: "Recibiendo de {0}", direct: "Conexión directa y cifrada entre los dos dispositivos", left: "faltan {0}",
    home: "Inicio", sentTo: "{0} archivos enviados a {1}.", recvFrom: "{0} archivos recibidos de {1}.",
    devName: "Nombre de este ordenador", devNameSub: "Lo ve el otro dispositivo", saveIn: "Guardar los archivos recibidos en", change: "Cambiar",
    askBefore: "Preguntar antes de recibir", askBeforeSub: "Muestra los archivos entrantes y pregunta si aceptarlos",
    language: "Idioma", menu: "Menú de aplicaciones", menuSub: "Añade PiqLink con su icono", quit: "Salir de PiqLink",
    infoText: "Transferencia directa de archivos entre ordenadores y teléfonos, sin servidores y sin registro. Compatible con PiqLink para Windows y Android.",
    close: "Cerrar", noFiles: "Elige primero los archivos que enviar.", uploading: "Preparando los archivos… {0}",
    dirPrompt: "Carpeta para los archivos recibidos:", closed: "PiqLink se ha cerrado. Puedes cerrar esta ventana.", lost: "Se perdió la conexión con PiqLink. Reinicia PiqLink."
  },
  zh: {
    useAssist: "使用辅助连接", replyTitle: "回复代码", replySub: "辅助连接：另一台设备会告诉你一个 10 位代码。请在这里输入。", confirmReply: "确认", replyOk: "回复代码已接受：正在连接…", replyShowTitle: "回复代码", replyShowSub: "把它告诉给你代码的人：对方在“回复代码”框中输入。", assistTitle: "辅助连接", consentText: "当路由器不接受来自互联网的连接时（移动网络中很常见），PiqLink 可以询问 Google 或 Cloudflare 的公共 STUN 服务器两台设备在互联网上的地址。然后两台设备之间建立直接通道。", consent1: "服务器只收到 IP 地址：没有文件，没有名称。", consent2: "文件始终在两台设备之间直接加密传输。", consent3: "你可以随时在设置中更改。", notNow: "暂不", allow: "允许", assistSet: "通过互联网的辅助连接", assistSetSub: "当路由器不开放端口时，公共服务器（STUN）帮助找到路径", stAssisting: "辅助连接：正在查找公网地址…",
    send: "发送", sendSub: "选择文件并输入代码", recv: "接收", recvSub: "生成代码或输入发送方的代码",
    settings: "设置", openDir: "打开文件夹", tagline: "文件从一台设备直接传到另一台。无需服务器。",
    menuAsk: "要把 PiqLink 及其图标添加到应用程序菜单吗？", noThanks: "不用了", add: "添加",
    dropTitle: "将文件拖到这里", dropSub: "或点击选择", addFiles: "添加文件", addDir: "添加文件夹",
    codeFromRecv: "接收方的代码", codeFromRecvSub: "接收方选择“接收”，并给你一个 6、8、12 或 14 位的代码。",
    sendGo: "发送", orShow: "或者由接收方输入代码：", genCode: "自己生成代码",
    lan: "同一网络", net: "通过互联网", haveCode: "我有发送方的代码", cancel: "取消",
    titleRecv: "把这个代码告诉发送方", titleSend: "把这个代码告诉接收方",
    hRecv: "接收", hSend: "用你的代码发送",
    stStarting: "正在准备代码…", stWaiting: "正在等待另一台设备…", stPeer: "已连接 {0}…", stWaitAccept: "正在等待 {0} 接受文件…",
    expires: " · {0} 过期",
    fwHint: "如果另一台设备找不到这台电脑，请检查防火墙：例如 sudo ufw allow 47760:47763/tcp",
    senderCode: "发送方的代码", senderCodeSub: "输入发送设备显示的代码（发送 › “自己生成代码”）。",
    connect: "连接", back: "返回", connecting: "正在连接…",
    askTitle: "{0} 想发送给你", nFiles: "{0} 个文件 · {1}", reject: "拒绝", accept: "接受", askLeft: "请在 {0} 内回答",
    sendingTo: "正在发送给 {0}", receivingFrom: "正在接收来自 {0}", direct: "两台设备之间的直接加密连接", left: "剩余 {0}",
    home: "首页", sentTo: "已向 {1} 发送 {0} 个文件。", recvFrom: "已从 {1} 接收 {0} 个文件。",
    devName: "这台电脑的名称", devNameSub: "另一台设备会看到它", saveIn: "接收的文件保存到", change: "更改",
    askBefore: "接收前询问", askBeforeSub: "显示传入的文件并询问是否接受",
    language: "语言", menu: "应用程序菜单", menuSub: "添加 PiqLink 及其图标", quit: "退出 PiqLink",
    infoText: "在电脑和手机之间直接传输文件，无需服务器，无需注册。与 Windows 和 Android 版 PiqLink 兼容。",
    close: "关闭", noFiles: "请先选择要发送的文件。", uploading: "正在准备文件… {0}",
    dirPrompt: "接收文件的文件夹：", closed: "PiqLink 已关闭。可以关闭此窗口。", lost: "与 PiqLink 的连接已断开。请重新启动 PiqLink。"
  }
};

function $(id) { return document.getElementById(id); }
var TOKEN = new URLSearchParams(location.search).get("t") || "";
var S = null, lang = "it";
function t(k) {
  var s = (TX[lang] && TX[lang][k]) || TX.it[k] || k;
  for (var i = 1; i < arguments.length; i++) s = s.split("{" + (i - 1) + "}").join(arguments[i]);
  return s;
}
function fmtSize(n) {
  var u = ["B", "KB", "MB", "GB", "TB"], i = 0;
  while (n >= 1000 && i < u.length - 1) { n /= 1000; i++; }
  return (i ? n.toFixed(n < 10 ? 1 : 0) : n) + " " + u[i];
}
function fmtTime(s) {
  s = Math.max(0, Math.round(s));
  var m = Math.floor(s / 60);
  return m ? m + ":" + String(s % 60).padStart(2, "0") : s + " s";
}
function act(a, arg, b, n) {
  return fetch("api/do", { method: "POST", headers: { "Content-Type": "application/json", "X-Piq-Token": TOKEN }, body: JSON.stringify({ act: a, arg: arg || "", bool: !!b, n: n || 0 }) }).catch(function () { });
}
function applyTexts() {
  document.documentElement.lang = lang;
  document.querySelectorAll("[data-t]").forEach(function (e) { e.textContent = t(e.getAttribute("data-t")); });
}
function fill(ul, items, onRemove) {
  ul.innerHTML = "";
  items.forEach(function (it, i) {
    var li = document.createElement("li"), n = document.createElement("span"), s = document.createElement("span");
    n.className = "n"; n.textContent = (it.dir ? "📁 " : "") + it.name;
    s.className = "s"; s.textContent = it.dir ? t("nFiles", it.files, fmtSize(it.size)) : fmtSize(it.size);
    li.append(n, s);
    if (onRemove) {
      var x = document.createElement("button");
      x.textContent = "×";
      x.onclick = function () { onRemove(i); };
      li.append(x);
    }
    ul.append(li);
  });
}
function qrSvg(rows) {
  if (!rows || !rows.length) return "";
  var n = rows.length, q = 4, d = "";
  for (var y = 0; y < n; y++) for (var x = 0; x < n; x++) if (rows[y][x] === "1") d += "M" + (x + q) + " " + (y + q) + "h1v1h-1z";
  var s = n + 2 * q;
  return '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ' + s + " " + s + '" shape-rendering="crispEdges"><rect width="100%" height="100%" fill="#fff"/><path d="' + d + '" fill="#000"/></svg>';
}

// ---------------------------------------------------------------- render
var speed = { at: 0, done: 0, rate: 0 }, lastScreen = "";
function render() {
  if (!S) return;
  if (S.lang !== lang) { lang = S.lang; applyTexts(); $("lang").value = lang; }
  var scr = S.screen, sec = { recv: "code", sendwait: "code" }[scr] || scr;
  document.querySelectorAll("section").forEach(function (s) { s.classList.toggle("hidden", s.id !== "s-" + sec); });
  if (scr !== lastScreen) { window.scrollTo(0, 0); lastScreen = scr; }
  $("ver").textContent = $("ver2").textContent = "v" + S.version + " Linux";
  $("infoVer").textContent = "Direct Transfer " + S.version + " · Linux";
  var sendSide = scr === "send" || scr === "sendwait" || (S.sending && (scr === "transfer" || scr === "result"));
  var recvSide = scr === "recv" || scr === "recvcode" || scr === "confirm" || (!S.sending && (scr === "transfer" || scr === "result"));
  $("navSend").classList.toggle("sel", sendSide);
  $("navRecv").classList.toggle("sel", recvSide);
  $("notice").textContent = S.notice || "";
  $("notice").classList.toggle("hidden", !S.notice);
  $("menuAsk").classList.toggle("hidden", !S.menuAsk);

  // send
  fill($("sendList"), S.items || [], S.busy ? null : function (i) { act("remove", "", false, i); });
  $("sendTotal").textContent = S.nfiles ? t("nFiles", S.nfiles, fmtSize(S.total)) : "";
  $("sendErr").textContent = S.sendErr || "";
  $("recvErr").textContent = S.sendErr || "";
  $("sendGo").disabled = $("sendShow").disabled = S.connecting;
  $("sendGo").textContent = S.connecting && S.sending ? (S.waitPeer ? t("stWaitAccept", S.peer || "…") : t("connecting")) : t("sendGo");
  $("recvGo").disabled = S.connecting;
  $("recvGo").textContent = S.connecting && !S.sending ? t("connecting") : t("connect");

  // code
  if (sec === "code") {
    var sending = scr === "sendwait";
    $("codeH").textContent = t(sending ? "hSend" : "hRecv");
    $("codeTitle").textContent = t(sending ? "titleSend" : "titleRecv");
    $("segLan").classList.toggle("on", S.mode !== "net");
    $("segNet").classList.toggle("on", S.mode === "net");
    $("codeBig").textContent = S.code || "···-···";
    var qr = qrSvg(S.qr);
    if ($("qr").getAttribute("data-c") !== S.code) { $("qr").innerHTML = qr; $("qr").setAttribute("data-c", S.code || ""); }
    var st = "", dot = "dot p";
    if (S.rstate === "starting") st = t(S.assisting ? "stAssisting" : "stStarting");
    else if (S.rstate === "waiting") {
      st = S.waitPeer ? t("stWaitAccept", S.peer) : (S.peer ? t("stPeer", S.peer) : t("stWaiting"));
      if (S.expires) st += t("expires", new Date(S.expires).toLocaleTimeString(lang, { hour: "2-digit", minute: "2-digit" }));
    } else if (S.rstate === "failed") { st = "—"; dot = "dot r"; }
    $("codeStatus").textContent = st;
    $("codeDot").className = dot;
    $("codeErr").textContent = S.rstate === "failed" ? S.rerr : "";
    $("haveCode").classList.toggle("hidden", sending);
    $("assistRow").classList.toggle("hidden", !(S.rstate === "failed" && S.needAssist));
    $("replyBox").classList.toggle("hidden", !(S.p2p && S.rstate === "waiting" && !S.waitPeer && !S.peer));
    $("replyMsg").textContent = S.replyMsg === "ok" ? t("replyOk") : (S.replyMsg || "");
    $("replyMsg").style.color = S.replyMsg === "ok" ? "var(--green)" : "";
  }
  document.querySelectorAll(".replyShow").forEach(function (e) {
    e.classList.toggle("hidden", !S.reply || !S.connecting);
    e.querySelector(".replyCode").textContent = S.reply || "";
  });

  // confirm
  if (scr === "confirm") {
    $("askTitle").textContent = t("askTitle", S.peer || "?");
    fill($("askList"), S.offer || []);
    var left = (S.askUntil - Date.now()) / 1000;
    $("askTotal").textContent = t("nFiles", (S.offer || []).length, fmtSize(S.offerTotal)) + " · " + t("askLeft", fmtTime(left));
  }

  // transfer
  if (scr === "transfer") {
    $("xTitle").textContent = t(S.sending ? "sendingTo" : "receivingFrom", S.peer || "…");
    $("xFile").textContent = S.pcount > 1 ? (S.pindex + 1) + "/" + S.pcount + " · " + (S.pfile || "") : (S.pfile || "");
    var pct = S.ptotal ? Math.min(100, S.done / S.ptotal * 100) : 0;
    $("xBar").style.width = pct.toFixed(1) + "%";
    $("xPct").textContent = Math.floor(pct) + "%";
    var now = Date.now();
    if (now - speed.at > 1000) {
      if (speed.at && S.done >= speed.done) speed.rate = speed.rate ? speed.rate * 0.6 + 0.4 * (S.done - speed.done) / ((now - speed.at) / 1000) : (S.done - speed.done) / ((now - speed.at) / 1000);
      speed.at = now; speed.done = S.done;
    }
    $("xSpeed").textContent = speed.rate > 0 ? fmtSize(speed.rate) + "/s" : "";
    $("xLeft").textContent = speed.rate > 0 && S.done < S.ptotal ? t("left", fmtTime((S.ptotal - S.done) / speed.rate)) : fmtSize(S.done) + " / " + fmtSize(S.ptotal);
  } else speed = { at: 0, done: 0, rate: 0 };

  // result
  if (scr === "result") {
    $("resTitle").textContent = S.resTitle;
    $("resText").textContent = S.resOK ? t(S.sending ? "sentTo" : "recvFrom", S.saved, S.resText) : S.resText;
    $("resIcon").className = "okc" + (S.resOK ? "" : " bad");
    $("resPath").setAttribute("d", S.resOK ? "M5 12.5l4.5 4.5L19 7.5" : "M7 7l10 10M17 7L7 17");
    $("resOpen").classList.toggle("hidden", !S.resOpen);
  }

  // settings
  if (scr === "settings") {
    if (document.activeElement !== $("setName")) $("setName").value = S.name;
    $("setDir").textContent = S.dir;
    $("setConfirm").checked = S.confirm;
    $("setAssist").checked = S.assisted;
  }
}

// confirm countdown
setInterval(function () { if (S && S.screen === "confirm") render(); }, 1000);

// ---------------------------------------------------------------- state stream
var closing = false, lostTimer = null;
function connectEvents() {
  var es = new EventSource("api/events?t=" + encodeURIComponent(TOKEN));
  es.onmessage = function (e) {
    clearTimeout(lostTimer); $("overlay").classList.add("hidden");
    S = JSON.parse(e.data); render();
  };
  es.onerror = function () {
    if (closing) return;
    clearTimeout(lostTimer);
    lostTimer = setTimeout(function () { $("overlayText").textContent = t("lost"); $("overlayBar").parentNode.classList.add("hidden"); $("overlay").classList.remove("hidden"); }, 4000);
  };
}

// ---------------------------------------------------------------- adding files from the page
// (when there is no zenity/kdialog, and for drag & drop): uploaded to the
// piqlink program on this computer, never to the Internet.
async function uploadAll(list) {
  if (!list.length) return;
  var total = list.reduce(function (s, x) { return s + x.file.size; }, 0), sent = 0, tops = {};
  $("overlayBar").parentNode.classList.remove("hidden");
  $("overlay").classList.remove("hidden");
  for (var i = 0; i < list.length; i++) {
    var x = list[i];
    $("overlayText").textContent = t("uploading", x.path);
    tops[x.path.split("/")[0]] = 1;
    await fetch("api/upload?path=" + encodeURIComponent(x.path), { method: "POST", headers: { "X-Piq-Token": TOKEN }, body: x.file });
    sent += x.file.size;
    $("overlayBar").style.width = (total ? sent / total * 100 : 100) + "%";
  }
  $("overlay").classList.add("hidden");
  act("uploaded", JSON.stringify(Object.keys(tops)));
}
function fromInput(files) {
  return Array.from(files).map(function (f) { return { file: f, path: f.webkitRelativePath || f.name }; });
}
async function fromDrop(dt) {
  var out = [], entries = [];
  for (var i = 0; i < dt.items.length; i++) {
    var en = dt.items[i].webkitGetAsEntry && dt.items[i].webkitGetAsEntry();
    if (en) entries.push(en);
  }
  if (!entries.length) return fromInput(dt.files);
  async function walk(en, prefix) {
    if (en.isFile) {
      var f = await new Promise(function (res, rej) { en.file(res, rej); });
      out.push({ file: f, path: prefix + en.name });
    } else if (en.isDirectory) {
      var rd = en.createReader(), batch;
      do {
        batch = await new Promise(function (res, rej) { rd.readEntries(res, rej); });
        for (var j = 0; j < batch.length; j++) await walk(batch[j], prefix + en.name + "/");
      } while (batch.length);
    }
  }
  for (var k = 0; k < entries.length; k++) await walk(entries[k], "");
  return out;
}

// ---------------------------------------------------------------- code fields
function normCode(s) { return String(s).toUpperCase().replace(/O/g, "0").replace(/[IL]/g, "1").replace(/U/g, "V").replace(/[^0-9A-Z]/g, ""); }
function fmtCode(c, deleting) {
  c = normCode(c).slice(0, 14);
  if (c.length <= 6) return c.length > 3 || (c.length === 3 && !deleting) ? c.slice(0, 3) + "-" + c.slice(3) : c;
  return c.match(/.{1,4}/g).join("-"); // 8 and 12 characters: groups of 4
}
function codeField(input, go) {
  var del = false;
  input.addEventListener("input", function (e) {
    del = !!(e.inputType && e.inputType.indexOf("delete") === 0);
    if (!e.isComposing) input.value = fmtCode(input.value, del);
  });
  input.addEventListener("keydown", function (e) { if (e.key === "Enter") go(); });
}

// ---------------------------------------------------------------- init
function pick(folder) {
  if (S && S.picker) act("pick", "", folder);
  else $(folder ? "pickDir" : "pick").click();
}
function init() {
  lang = (navigator.language || "it").slice(0, 2);
  if (!TX[lang]) lang = "en";
  applyTexts();
  $("navSend").onclick = $("homeSend").onclick = function () { act("screen", "send"); };
  $("navRecv").onclick = $("homeRecv").onclick = function () { act("recv", "lan"); };
  $("navSet").onclick = function () { act("screen", "settings"); };
  $("navInfo").onclick = function () { $("info").showModal(); };
  $("navOpen").onclick = function () { act("open", "dir"); };
  $("menuYes").onclick = $("setMenu").onclick = function () { act("menu", "", true); };
  $("menuNo").onclick = function () { act("menu", "", false); };
  $("homeDrop").onclick = $("drop").onclick = $("addFiles").onclick = function () { pick(false); };
  $("addDir").onclick = function () { pick(true); };
  $("pick").onchange = function () { uploadAll(fromInput(this.files)); this.value = ""; };
  $("pickDir").onchange = function () { uploadAll(fromInput(this.files)); this.value = ""; };
  ["dragenter", "dragover"].forEach(function (ev) { document.addEventListener(ev, function (e) { e.preventDefault(); $("drop").classList.add("on"); }); });
  ["dragleave", "drop"].forEach(function (ev) { document.addEventListener(ev, function (e) { e.preventDefault(); $("drop").classList.remove("on"); }); });
  document.addEventListener("drop", function (e) {
    if (!e.dataTransfer || (S && S.busy)) return;
    fromDrop(e.dataTransfer).then(uploadAll);
  });
  function goSend() {
    if (!S || !S.items || !S.items.length) { $("sendErr").textContent = t("noFiles"); return; }
    act("connect", $("sendCode").value, true);
  }
  codeField($("sendCode"), goSend);
  codeField($("recvCode"), function () { act("connect", $("recvCode").value, false); });
  $("sendGo").onclick = goSend;
  $("sendShow").onclick = function () {
    if (!S || !S.items || !S.items.length) { $("sendErr").textContent = t("noFiles"); return; }
    act("sendwait", "lan");
  };
  $("segLan").onclick = function () { act(S.screen === "sendwait" ? "sendwait" : "recv", "lan"); };
  $("segNet").onclick = function () { act(S.screen === "sendwait" ? "sendwait" : "recv", "net"); };
  $("haveCode").onclick = function () { act("screen", "recvcode"); };
  $("codeCancel").onclick = $("xCancel").onclick = function () { act("cancel"); };
  $("recvBack").onclick = function () { act("recv", "lan"); };
  $("recvGo").onclick = function () { act("connect", $("recvCode").value, false); };
  $("askYes").onclick = function () { act("answer", "", true); };
  $("askNo").onclick = function () { act("answer", "", false); };
  $("resHome").onclick = function () { act("screen", "home"); };
  $("resOpen").onclick = function () { act("open"); };
  $("setName").onchange = function () { act("name", this.value); };
  $("setConfirm").onchange = function () { act("confirm", "", this.checked); };
  $("setAssist").onchange = function () { act("assisted", "", this.checked); };
  $("assistBtn").onclick = function () { act("assist"); };
  $("consYes").onclick = function () { act("consent", "", true); };
  $("consNo").onclick = function () { act("consent", "", false); };
  $("replyGo").onclick = function () { act("reply", $("replyIn").value); };
  $("replyIn").addEventListener("input", function () {
    var c = normCode(this.value).slice(0, 10);
    this.value = c.length > 5 ? c.slice(0, 5) + "-" + c.slice(5) : c;
  });
  $("replyIn").addEventListener("keydown", function (e) { if (e.key === "Enter") act("reply", $("replyIn").value); });
  $("lang").onchange = function () { act("lang", this.value); };
  $("setDirBtn").onclick = function () {
    if (S && S.picker) act("pickdir");
    else { var p = prompt(t("dirPrompt"), S ? S.dir : ""); if (p) act("setdir", p); }
  };
  $("quit").onclick = function () {
    closing = true; act("quit");
    $("overlayText").textContent = t("closed"); $("overlayBar").parentNode.classList.add("hidden"); $("overlay").classList.remove("hidden");
    setTimeout(function () { window.close(); }, 600);
  };
  connectEvents();
}
init();
