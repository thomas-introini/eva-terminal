# EVA Terminal — piano di implementazione Umami per Codex

Repository: https://github.com/thomas-introini/eva-terminal
Data del piano: 6 ottobre 2026. Basato sul branch `main` letto in questa conversazione. Prima di implementare, leggere `AGENTS.md`, il codice e i test della revisione effettivamente disponibile; adattare i punti di integrazione se nel frattempo sono cambiati.

## Obiettivo e ambito

Implementare analytics della navigazione SSH e delle azioni commerciali con Umami, senza rallentare la TUI o interferire con carrello, checkout, webhook e recovery. Usare un website Umami dedicato **EVA Terminal**, separato da evacaffe.it, nella stessa istanza. Tenere separato anche lo staging. Non modificare il tracking del sito WooCommerce.

Il progetto usa Go, Wish e Bubble Tea, un catalogo locale condiviso e un carrello persistente per chiave SSH verificata. Connessioni con la stessa chiave condividono il carrello. Stripe Checkout viene creato dal gateway WordPress; webhook, polling e cron riconciliano il pagamento. Gli analytics del browser Woo non osservano la navigazione locale della TUI.

Completare tutte le fasi di questo piano, inclusi acquisti offline, test e documentazione. Non aggiungere PostHog, un tracker JavaScript, un nuovo servizio di raccolta o dipendenze esterne non necessarie. Refund analytics, retention tra riconnessioni e tracking di ogni tasto sono fuori ambito.

## 1. Verificare il contratto della versione Umami installata

Prima di scegliere i dettagli del trasporto, accertare la versione dell'istanza di destinazione. Se non accessibile, implementare un adapter con un contratto esplicito e lasciare la verifica reale come prerequisito del rollout; non dichiarare verificata la compatibilità.

- Usare `POST /api/send`, JSON e un User-Agent accettato, per esempio `EvaTerminal/1.0`. Non falsificare browser, sistema operativo, geolocalizzazione o dimensioni dello schermo. Non inoltrare IP SSH né header `X-Forwarded-For` del cliente.
- Le pageview usano `type: "event"` senza `payload.name`; le azioni usano lo stesso tipo con `payload.name` e `payload.data`. Verificare la classificazione effettiva sull'istanza.
- Verificare il comportamento di `payload.id`, della risposta e dell'eventuale cache/header usato dal tracker della versione installata. Un ID nelle proprietà `data` da solo non isola le sessioni native di Umami.
- Non presumere che `/api/batch`, timestamp retrodatati, revenue automatiche o idempotency key siano disponibili. Per la prima versione, inviare un evento per richiesta.
- Testare due connessioni dallo stesso server: devono risultare distinguibili. Testare che gli eventi dello stesso percorso risultino collegati nei report previsti. Se il contratto non lo consente, documentare il limite prima di dichiarare attendibili funnel e visitatori.

Esempio indicativo di un'azione; i dettagli di identità restano subordinati alla verifica del contratto:

```json
{
  "type": "event",
  "payload": {
    "website": "<website UUID di EVA Terminal>",
    "hostname": "eva-terminal",
    "url": "/shop",
    "title": "Shop",
    "referrer": "",
    "id": "<UUID casuale della connessione>",
    "name": "view_item",
    "data": {
      "channel": "ssh",
      "schema_version": 1,
      "environment": "production",
      "connection_id": "<stesso UUID>",
      "product_id": 123
    }
  }
}
```

Non riutilizzare la cache di identità globalmente tra clienti. Il worker deve mantenere l'ordine FIFO per connessione. Non inviare credenziali amministrative Umami: l'endpoint di raccolta non richiede il token dell'API amministrativa.

## 2. Identità e attribuzione

- Generare con un generatore crittografico un UUID casuale per ogni connessione SSH autenticata che avvia la TUI. Due connessioni con la stessa chiave hanno due UUID diversi; non cambiare l'identità del carrello esistente.
- Usare questo UUID nell'identità supportata dall'adapter e nelle proprietà degli eventi. Non inviare fingerprint, chiave SSH, username, `customer_ref`, Cart-Token, email o indirizzo.
- Una riconnessione genera un nuovo UUID. In questa versione si misurano **connessioni**, non persone uniche o utenti di ritorno. Scriverlo nella documentazione e nei nomi dei report.
- Al primo submit di un nuovo tentativo di checkout, fissare l'attribuzione analytics del tentativo alla connessione che lo crea. Persistirla insieme alla richiesta prima del primo invio HTTP. Retry, restart e riconnessioni riutilizzano esattamente quel contesto; non sostituirlo con il nuovo UUID.
- Trasmettere al bridge un contesto opzionale, con schema ristretto: UUID, versione schema, environment e flag di raccolta. Persistirlo sul tentativo/ordine con CRUD Woo compatibile HPOS. La configurazione di host e website ID è amministrativa sul gateway: non accettare URL o website arbitrari dal body del checkout.
- Il request hash del bridge considera attualmente l'intero body: il contesto va congelato e incluso nella richiesta durabile. Le vecchie richieste prive di analytics restano riproducibili senza riscritture o variazioni dell'hash.
- Contesto assente o analytics malformati non devono bloccare un pagamento valido: ignorare la parte analytics e registrare solo una diagnostica sintetica. Non attribuire artificialmente acquisti legacy a una connessione nuova.
- Un pagamento avvenuto dopo la disconnessione conserva l'attribuzione originaria, ma può cadere fuori dalla finestra temporale del funnel nativo. Conservare il collegamento esplicito e descrivere questa differenza; non promettere la continuazione di una visita browser né una fusione con la sessione Stripe.

## 3. Pageview virtuali

| Stato effettivamente visibile | Path | Titolo |
| --- | --- | --- |
| `ViewProductList` | `/shop` | Shop |
| `ViewCart` | `/cart` | Cart |
| `ViewAddress` | `/checkout/address` | Checkout address |
| `ViewShipping` | `/checkout/shipping` | Checkout shipping |
| `ViewReview` | `/checkout/review` | Checkout review |
| `ViewOrderConfirmation` | `/checkout/payment` | Checkout payment |

Emettere una pageview al primo schermo commerciale visibile e quando cambia lo schermo visibile. Nessun evento dentro `View()` o funzioni di rendering. Non contare splash, spinner, resize, scroll, blink, help overlay o aggiornamenti di polling.

Centralizzare l'osservazione dello stato dopo l'elaborazione del messaggio Bubble Tea, includendo tutti gli early return. Non basarsi solo sulle azioni da tastiera: transizioni avvengono anche dopo prepared/shipping/checkout messages e durante il ripristino. In `preparedMsg` può esserci un passaggio interno per Review prima di Shipping: contare solo lo stato finale mostrato.

Sotto le dimensioni minime, sospendere la rilevazione delle schermate nascoste; contare quella effettivamente mostrata quando il terminale torna utilizzabile. Una riconnessione con pagamento pending può iniziare direttamente da `/checkout/payment`: non inventare una visita a `/shop`.

## 4. Eventi e semantica

Tutti gli eventi hanno proprietà piatte e consentite esplicitamente: `channel=ssh`, `schema_version=1`, `environment`, `connection_id`. Aggiungere solo quelle previste nella tabella. Non serializzare struct applicative, payload Woo o errori grezzi.

| Evento | Trigger | Proprietà specifiche |
| --- | --- | --- |
| `session_started` | Una volta quando inizia la TUI commerciale | `resumed_payment` boolean |
| `view_item` | Dettagli di un prodotto visibili e selezione stabile per 700 ms | `product_id` |
| `search` | Query non vuota confermata oppure stabile per 700 ms mentre la ricerca è visibile | `query_length`, `results_count` |
| `filter_changed` | Cambio volontario del filtro stock | `in_stock_only` |
| `add_to_cart` | Intento locale accettato da `shopper.Add`, una volta per azione | `product_id`, eventuale `variation_id`, `quantity`, `grind`, `confirmation=local_intent` |
| `remove_from_cart` | Intento di eliminazione o decremento accettato localmente | ID prodotto/variante disponibili, `quantity` rimossa, `confirmation=local_intent` |
| `cart_quantity_changed` | Incremento accettato localmente su una riga già presente | ID disponibili, `quantity_before`, `quantity_after`, `confirmation=local_intent` |
| `begin_checkout` | Azione valida che entra nel checkout dal carrello | `item_count` |
| `coupon_applied` / `coupon_removed` | Operazione confermata da Woo | Nessun codice coupon |
| `shipping_selected` | Scelta confermata da Woo | `shipping_method` normalizzato e consentito |
| `checkout_submitted` | Primo tentativo durabile di creazione pagamento | `checkout_id` casuale non utilizzabile come credenziale |
| `payment_link_available` | Link disponibile per la prima volta nel tentativo | `checkout_id` |
| `payment_link_copy_requested` | Richiesta esplicita OSC 52 | Nessun URL; non chiamarlo link aperto o copia riuscita |
| `purchase` | Gateway conferma il pagamento autorevolmente | `event_id`, `checkout_id`, `revenue`, `currency`, `item_count` |
| `checkout_error` | Operazione commerciale fallita, non ogni polling | `stage`, `error_code` da allowlist |

Regole:

- `view_item`: deduplicare per prodotto per connessione; ignorare messaggi timer obsoleti con un contatore/generation ID. Attivare anche per il primo prodotto selezionato dopo splash; nessuna impression mentre dettagli non visibili.
- `search`: non inviare il testo, deduplicare internamente query identiche consecutive e ignorare query cancellata/timer obsoleti. La query resta solo in memoria locale; `results_count` riguarda risultati locali.
- Non inventare il parent product ID quando nel carrello compare solo l'ID variante: preservare il mapping o documentare e tipizzare correttamente il campo. `grind` deve essere un valore catalogo normalizzato, non testo arbitrario.
- Le mutazioni del carrello sono **intenti locali**, non prova dell'accettazione Woo: la sincronizzazione è asincrona e può coalescere o correggere quantità. Errori Woo generano un evento separato classificato. Esplicitare questa semantica nei report.
- Non generare azioni durante `applyShopper`, broadcast del carrello condiviso, refresh catalogo o ripristino. Solo la connessione che agisce emette l'azione.
- Deduplicare `checkout_submitted` e `payment_link_available` sul tentativo durabile, non sul ciclo di rendering. Un nuovo tentativo può generare nuovi eventi; un retry no.
- `begin_checkout` conta gli ingressi volontari validi, anche un rientro successivo; documentare che il funnel deve usare le connessioni che raggiungono uno step, non dividere conteggi grezzi di click.
- `purchase.revenue` viene dal totale finale Woo, incluse tasse/spedizione, espresso come numero in unità principali. Conversione dai minor units controllata, mai dal prezzo visualizzato o da un calcolo locale. Valuta autorevole dell'ordine. Non assumere che questi campi attivino automaticamente un report revenue in ogni versione.
- Ordini zero-total generano `purchase` con revenue 0 dopo completamento autorevole. Test mode non invia al website di produzione.

## 5. Client Go e configurazione

Creare `internal/analytics/` con interfaccia minima, implementazione no-op, adapter Umami, validazione schema, coda e clock/trasporto iniettabili per test. Esempio orientativo:

```go
type Tracker interface {
    PageView(page PageView) bool
    Event(event Event) bool
}
```

Il risultato indica solo accettazione nella coda locale; la TUI non deve aspettare una risposta HTTP. Tutte le chiamate devono essere non bloccanti. Un worker condiviso, coda FIFO limitata a 512 eventi, timeout HTTP 2 secondi; nessuna goroutine per evento e nessuna coda illimitata. Su overflow scartare l'evento e incrementare un contatore locale. Eventi di navigazione best effort, nessun retry automatico su esito ambiguo e nessun replay delle visite dopo un restart.

Shutdown: smettere di accettare eventi e tentare un drain fino a 3 secondi, senza allungare indefinitamente la chiusura SSH. Il worker usa il contesto del processo, non quello della singola connessione, così un disconnect non cancella istantaneamente eventi già accodati. Nessun blocco dell'UI in caso di errore o tracking disabilitato.

Configurazione Go proposta:

```dotenv
UMAMI_ENABLED=false
UMAMI_BASE_URL=
UMAMI_WEBSITE_ID=
UMAMI_HOSTNAME=eva-terminal
UMAMI_ENVIRONMENT=development
```

Website ID validato come UUID; URL HTTP(S) senza userinfo/query/fragment, HTTPS in produzione; HTTP consentito solo per mock locale. Redirect verso altri host rifiutati. Flag false = zero traffico, zero worker e no-op. Configurazione analytics errata = diagnostica e tracking disabilitato, senza disabilitare negozio/checkout. `UMAMI_ENVIRONMENT` ha allowlist development/staging/production. Per dev nessuna destinazione reale di default.

Non aggiungere opzioni per tutti i dettagli interni: timeout, debounce, queue size e drain possono essere costanti documentate. Hostname è una convenzione analytics, non una risoluzione DNS necessaria. Non inviare colonne×righe del terminale come pixel di `screen`. Omettere campi non applicabili se il contratto lo consente.

Diagnostica: contatori accepted/sent/dropped/failed/uncertain, log sintetici limitati nel tempo. Mai loggare JSON completo, query, token, pagamento o anagrafiche.

## 6. Gateway WordPress: acquisti anche offline

Configurare sul gateway, tramite costanti/server environment documentate, enable flag, Umami base URL, website ID, hostname e environment coerenti con Go. La disattivazione analytics non deve disattivare webhook, Stripe o recovery.

Creare `includes/analytics.php` ed una outbox persistente separata per eventi purchase, compatibile con classic orders e HPOS. Non inviare HTTP durante la transazione checkout o mentre un lock di pagamento è detenuto. Non emettere `purchase` dalla TUI, da Stripe return URL, dalla creazione ordine o dalla sola esistenza del link.

Il codice ha più percorsi che diventano paid: `reconcile()`, zero-total in `payment()`, checkout già pagato e cancellation che scopre un pagamento vincente. Centralizzare la rilevazione o collegarsi al completamento autorevole con una recovery che copra tutti questi percorsi. Inserire al massimo una riga outbox per ordine con vincolo UNIQUE, payload minimo immutabile, ID evento casuale stabile e stato iniziale pending. Non usare solo un flag in memoria o un meta write non atomico per deduplicare.

Un job cron scansisce in batch limitati anche gli ordini/tentativi paid attribuiti ma senza outbox: l'attuale cron del bridge scansisce soprattutto pending, quindi da solo non copre un crash tra payment completion e enqueue. Non inviare automaticamente vecchi ordini senza contesto analytics; usare un cutoff di attivazione o un flag eleggibilità. La produzione deve usare il cron reale già richiesto dal gateway.

### Limite di consegna e politica per evitare conteggi gonfiati

Un vincolo locale UNIQUE evita doppi enqueue da webhook/recovery, ma non garantisce exactly-once remoto. `event_id` come proprietà non prova che Umami deduplichi. Un timeout può verificarsi dopo l'inserimento remoto.

Per questa prima versione privilegiare l'assenza di retry ambigui:

1. Claim atomico dell'outbox e transizione persistente a `sending` prima della richiesta; un solo sender per riga.
2. Risposta valida di successo secondo il contratto verificato → `sent`.
3. Fallimento dimostrabilmente prima dell'invio, o rifiuto che il contratto garantisce non inserisca eventi → retry limitato con backoff, massimo 3 tentativi.
4. Timeout/reset/5xx con esito incerto o crash durante `sending` → `uncertain`, nessun reinvio automatico. Documentare anche la possibile perdita quando il crash precede la richiesta. Una riga `sending` abbandonata diventa uncertain, non pending.
5. Esaurimento dei tentativi certi → `failed`; log sintetico, nessun impatto sull'ordine.

Questo fornisce deduplicazione interna e riduce il rischio di doppi acquisti su Umami, con possibile sottoconteggio. Woo resta la fonte dei ricavi e degli ordini. Non chiamare il risultato exactly-once. Un replay manuale non fa parte della prima versione. Se la versione installata supporta una vera idempotency key documentata, usarla e aggiornare test/politica; non inventarla.

Proteggere outbox e contesto con gli stessi criteri dei dati del gateway. Nessun payment URL/order key/Stripe ID necessario nel payload Umami. Conservare solo ID analytics, somme, valuta e conteggio prodotti. Retention e cleanup outbox documentati; conservare un marker di deduplicazione quando si cancellano i payload, così il cron non ricrea acquisti già elaborati.

## 7. Mappa dei punti di integrazione

| File/area | Modifica prevista |
| --- | --- |
| `internal/analytics/` nuovo | Schema, no-op, adapter HTTP, coda e diagnostica |
| `internal/config/config.go`, `.env.example` | Flag e configurazione analytics |
| `cmd/woossh/main.go` | Worker di processo, UUID per connessione e shutdown |
| `internal/tui/model.go` | Osservazione delle transizioni, azioni e timer analytics |
| `internal/tui/shop.go` | Selezione stabile prodotto senza effetti nel rendering |
| `internal/tui/storefront.go` | Risultati coupon/shipping, checkout e link disponibile |
| `internal/storefront/` | Contesto checkout congelato nella richiesta durabile; marker per eventi del tentativo |
| `internal/storeapi/` | Contesto opzionale nel body del bridge, compatibilità richieste legacy |
| `wordpress/eva-terminal-gateway/includes/bridge.php` | Persistenza contesto e raccordo con paid/recovery/cron |
| `wordpress/eva-terminal-gateway/includes/analytics.php` nuovo | Outbox, claim e invio HTTP fuori dai lock |
| Entry point gateway e install/schema | Caricamento analytics, migrazione idempotente e cron |
| README + `docs/terminal-analytics.md` nuovo | Setup, semantica, report, limiti e rollout |

Percorsi e simboli sono punti di partenza, non una richiesta di refactor generale. Conservare contratti di pagamento e recovery esistenti. Non includere analytics nelle quote economiche o nelle Stripe idempotency key se non già parte della richiesta congelata.

## 8. Test significativi e criteri di accettazione

Usare mock HTTP locale e fake clock/timer dove utile; nessun test deve inviare dati alla produzione.

1. Tracking false o malconfigurato: zero richieste; browsing/checkout funzionanti.
2. Pageview corrette per flusso completo, early return, rientro e reconnect pending. Cento redraw/resize/poll non moltiplicano eventi. Splash/help/terminal troppo piccolo non generano false view.
3. Selezione rapida A→B→C: solo C dopo 700 ms; cambio schermo invalida il timer; ritorno allo stesso prodotto non duplica `view_item`.
4. Ricerca: debounce, clear, paste, risultati zero; il payload non contiene testo query.
5. Due connessioni con stessa chiave condividono carrello ma non identità analytics. La mutazione produce una sola action dalla connessione agente; broadcast e sync non generano copie.
6. Mutazioni locali accettate/non accettate e successivo rifiuto Woo: eventi coerenti con semantica dichiarata; quantità clampate o invarianti non producono azioni spurie.
7. Queue piena, endpoint lento/offline, HTTP error e shutdown: memoria limitata, nessun blocco UI, nessuna perdita di goroutine; FIFO per connessione.
8. Payload allowlist: nessun dato anagrafico, token, fingerprint, query, link o errore grezzo. Revenue controllata, valuta Woo, zero-total.
9. Retry checkout/restart/reconnect: body/hash e contesto attribuzione immutabili. Richieste legacy senza analytics restano valide.
10. Webhook duplicati, fuori ordine, polling/cron simultanei: un solo enqueue purchase; pagamento a SSH disconnesso funziona.
11. Crash dopo paid ma prima enqueue: scan recovery crea la riga mancante. Paid zero-total e pagamento che vince sulla cancellazione sono coperti.
12. Crash/timeout dopo possibile invio: uncertain senza reinvio automatico; claim concorrenti non inviano due volte; cleanup non ricrea purchase.
13. Gateway HPOS e classic storage; migrazione e cron activation idempotenti; installazione nuova e aggiornamento da schema esistente.

Eseguire i check appropriati già presenti: `make test`, `go test -race ./...`, `go vet ./...`, `make gateway-check`, `make gateway-integration` quando disponibili. Aggiungere i casi analytics alla suite PHP/integrazione esistente. Riportare qualsiasi check non eseguibile e la causa; non chiamare verificati webhook reali tramite sole simulazioni.

## 9. Rollout e deliverable dell'agente

1. Preparare integrazione, test e documentazione con tracking spento per default.
2. Configurare website staging su Umami della stessa versione della produzione; configurare Go e gateway sullo stesso website staging.
3. Fare uno smoke test reale: due sessioni SSH distinte; prodotti/carrello/checkout; Stripe test; chiudere SSH prima del pagamento e verificare purchase dal gateway; controllare payload e conteggi in Umami.
4. Verificare classificazione pageview/eventi e isolamento identità sullo stesso IP server. Un mock HTTP non certifica report Umami.
5. Preparare funnel principale `/shop` → `view_item` → `add_to_cart` → `begin_checkout` → `purchase`, se la versione permette un funnel misto. Altrimenti usare step tutti-eventi con una semantica documentata. Delivery non è uno step obbligatorio: può essere omesso dal flusso.
6. Preparare report prodotti visualizzati/aggiunti, checkout avviati/acquisti, distribuzione delle schermate di ultimo evento. L'abbandono è un'inferenza dopo una finestra temporale dichiarata, non un evento certo sul disconnect. Sessioni che riaprono un payment sono un segmento distinto.
7. Abilitare il website produzione dopo verifica con configurazione esplicita. Questa task produce codice reviewabile e istruzioni; non richiede deployment, modifiche a segreti live o creazione automatica di risorse esterne.

Deliverable: codice e migrazioni, test, `.env.example`, `docs/terminal-analytics.md`, breve descrizione PR e risultati dei check. Includere nel documento i passi manuali Umami e le configurazioni gateway. Segnalare in particolare limiti delle connessioni come proxy degli utenti, finestre funnel per pagamento offline, consegna best effort e possibili sottoconteggi purchase. Non lasciare stub `purchase` come lavoro futuro.

## Fonti e codice esaminato

- Umami v2 collection API: https://v2.umami.is/docs/api/sending-stats
- Umami v2 distinct IDs: https://v2.umami.is/docs/distinct-ids
- Umami collection API corrente: https://docs.umami.is/docs/api/sending-stats
- Umami server-side tracking: https://docs.umami.is/docs/guides/send-server-side-events
- Repository README: https://github.com/thomas-introini/eva-terminal/blob/main/README.md
- Runtime e rollout: https://github.com/thomas-introini/eva-terminal/blob/main/docs/terminal-checkout.md
- TUI: https://github.com/thomas-introini/eva-terminal/blob/main/internal/tui/model.go
- Preview prodotti: https://github.com/thomas-introini/eva-terminal/blob/main/internal/tui/shop.go
- Storefront TUI: https://github.com/thomas-introini/eva-terminal/blob/main/internal/tui/storefront.go
- SSH bootstrap: https://github.com/thomas-introini/eva-terminal/blob/main/cmd/woossh/main.go
- Gateway: https://github.com/thomas-introini/eva-terminal/blob/main/wordpress/eva-terminal-gateway/includes/bridge.php

Le scelte di debounce, coda, eventi e gestione della consegna sono decisioni progettuali di questo piano. Non sono garanzie fornite da Umami. La versione installata non è stata accertata in questa conversazione.
