// ignore: unused_import
import 'package:intl/intl.dart' as intl;

import 'app_localizations.dart';

// ignore_for_file: type=lint

/// The translations for Italian (`it`).
class AppLocalizationsIt extends AppLocalizations {
  AppLocalizationsIt([String locale = 'it']) : super(locale);

  @override
  String get appTitle => 'pi-ui';

  @override
  String get navSessions => 'Sessioni';

  @override
  String get navFiles => 'File';

  @override
  String get navSearch => 'Cerca';

  @override
  String get navSettings => 'Impostazioni';

  @override
  String get retry => 'Riprova';

  @override
  String get reload => 'Ricarica';

  @override
  String get cancel => 'Annulla';

  @override
  String get close => 'Chiudi';

  @override
  String get save => 'Salva';

  @override
  String get send => 'Invia';

  @override
  String get delete => 'Elimina';

  @override
  String get refresh => 'Aggiorna';

  @override
  String get search => 'Cerca';

  @override
  String get selectASession => 'Scegli una sessione';

  @override
  String get pickASessionHint =>
      'Scegli una sessione a sinistra, o avviane una nuova.';

  @override
  String get noSessionsTitle => 'Nessuna sessione';

  @override
  String get noSessionsMessage =>
      'Una sessione è un processo pi in una directory di lavoro.';

  @override
  String get selectAFile => 'Scegli un file';

  @override
  String get pickAFileHint => 'Scegli un file a sinistra per leggerlo qui.';

  @override
  String get noWorkspaceTitle => 'Nessun workspace';

  @override
  String get noWorkspaceMessage =>
      'Questo server non ha un workspace da esplorare.';

  @override
  String get noWorkspaceFlagHint =>
      'Il server è stato avviato senza --root, quindi non espone alcun filesystem.';

  @override
  String get emptyDirectoryTitle => 'Directory vuota';

  @override
  String get emptyDirectoryMessage => 'Non c\'è nulla qui.';

  @override
  String get directoryFailedTitle => 'Non riesco a elencare quella directory';

  @override
  String get fileFailedTitle => 'Non riesco a leggere quel file';

  @override
  String get binaryFileTitle => 'File binario';

  @override
  String get reloadFromHost => 'Ricarica dall\'host';

  @override
  String get filesTitle => 'File';

  @override
  String get searchTitle => 'Cerca';

  @override
  String get searchHint => 'una parola, un percorso o un messaggio';

  @override
  String get searchScopeFiles => 'file';

  @override
  String get searchScopeMessages => 'messaggi';

  @override
  String get searchNow => 'Cerca ora';

  @override
  String get searchIntroTitle => 'Cerca sull\'host';

  @override
  String get searchIntroMessage =>
      'Scrivi almeno due caratteri. I file vengono dai workspace, i messaggi dalle sessioni pi che il server conosce.';

  @override
  String get searchNoMatchTitle => 'Nessun risultato';

  @override
  String get searchNoMatchMessage => 'Nulla corrisponde in quegli ambiti.';

  @override
  String get searchFailedTitle => 'La ricerca è fallita';

  @override
  String get settingsTitle => 'Impostazioni';

  @override
  String get settingsDeviceSection => 'Questo dispositivo';

  @override
  String get settingsPolicySection => 'Politica del server';

  @override
  String get settingsUpdatesSection => 'Aggiornamenti';

  @override
  String get settingsServerLabel => 'Server';

  @override
  String get settingsDeviceLabel => 'Dispositivo';

  @override
  String get settingsServerVersionLabel => 'Versione del server';

  @override
  String get settingsPiVersionLabel => 'versione di pi';

  @override
  String get settingsFeaturesLabel => 'Funzionalità';

  @override
  String get settingsCertificateLabel => 'Certificato';

  @override
  String get settingsConnectionLabel => 'Connessione';

  @override
  String get settingsNotPaired => 'non accoppiato';

  @override
  String get settingsUnknown => 'sconosciuto';

  @override
  String get settingsKeystoreNote =>
      'Il token del dispositivo vive nel keystore del sistema operativo; il server ne conserva solo l\'hash.';

  @override
  String get settingsNoSettings =>
      'Questo server non ha impostazioni: è stato avviato senza una directory di stato.';

  @override
  String settingsAdminHint(String scope) {
    return 'Per cambiarle serve un dispositivo admin; questo è un $scope.';
  }

  @override
  String get settingsChanged => 'modificata';

  @override
  String get settingsResetTooltip => 'Torna al valore predefinito';

  @override
  String settingsReadFailed(String error) {
    return 'Non riesco a leggere le impostazioni: $error';
  }

  @override
  String get updatesManagedNote =>
      'Questo deployment gestisce i propri aggiornamenti: il server non esegue alcuno script.';

  @override
  String get updatesApply => 'Esegui l\'aggiornamento';

  @override
  String updatesApplyStarted(String id) {
    return 'Aggiornamento avviato: task $id. L\'output è sull\'endpoint dei task.';
  }

  @override
  String get updatesApplyStartedNoId => 'Aggiornamento avviato.';

  @override
  String updatesInstalledLatest(String current, String latest) {
    return 'installato $current · disponibile $latest';
  }

  @override
  String updatesInstalledUnknown(String current, String error) {
    return 'installato $current · non verificato ($error)';
  }

  @override
  String updatesChecked(String when) {
    return 'verificato $when';
  }

  @override
  String get updatesAvailable => 'aggiorna';

  @override
  String updatesReadFailed(String error) {
    return 'Non riesco a leggere le versioni: $error';
  }

  @override
  String get connectionConnecting => 'Connessione al server…';

  @override
  String get connectionLost => 'Connessione persa: riprovo…';

  @override
  String get connectionIdle => 'Non connesso.';

  @override
  String get connectionRetry => 'Riprova';

  @override
  String get sessionsTitle => 'Sessioni';

  @override
  String get newSession => 'Nuova sessione';

  @override
  String get newSessionHint =>
      'pi gira in una directory dell\'host; il client non legge mai le chiavi dei provider.';

  @override
  String get workingDirectory => 'Directory di lavoro';

  @override
  String get workingDirectoryHint => '/home/utente/Projects/mio-progetto';

  @override
  String get workingDirectoryRequired => 'Serve una directory di lavoro.';

  @override
  String get nameOptional => 'Nome (facoltativo)';

  @override
  String get nameHint => 'pi-ui';

  @override
  String get create => 'Crea';

  @override
  String get newSessionTooltip => 'Nuova sessione';

  @override
  String get stopSession => 'Ferma la sessione';

  @override
  String get stopSessionTitle => 'Fermare questa sessione?';

  @override
  String stopSessionMessage(String cwd) {
    return 'A pi in $cwd viene chiesto di chiudersi. La conversazione resta su disco e la sessione può essere ripresa.';
  }

  @override
  String get stopSessionConfirm => 'Ferma';

  @override
  String get sessionsFailedTitle => 'Non riesco a caricare le sessioni';

  @override
  String get noEventsYet => 'nessun evento';

  @override
  String messagesCount(String count) {
    return '$count msg';
  }

  @override
  String queuedCount(String count) {
    return '$count in coda';
  }

  @override
  String get sessionActions => 'Azioni della sessione';

  @override
  String get rename => 'Rinomina…';

  @override
  String get renameTitle => 'Rinomina la sessione';

  @override
  String get clone => 'Clona';

  @override
  String get export => 'Esporta…';

  @override
  String get removeFromList => 'Togli dalla lista';

  @override
  String get chatNotConnected => 'Non connesso al server.';

  @override
  String get composerHintIdle => 'Scrivi a pi… (/ per i comandi)';

  @override
  String get composerHintStreaming => 'Guida il turno o lascia un follow-up';

  @override
  String get composerHintOffline =>
      'Riconnessione — il messaggio aspetta, non si perde';

  @override
  String get composerSend => 'Invia';

  @override
  String get composerSteer => 'Guida';

  @override
  String get composerFollowUp => 'Follow-up';

  @override
  String get composerQueue => 'Accoda';

  @override
  String get composerChooseMode => 'Scegli guida o follow-up';

  @override
  String get composerSteeringHint => 'La sessione sta rispondendo';

  @override
  String get composerSteerDetail =>
      'consegnato dopo le chiamate ai tool in corso';

  @override
  String get composerFollowUpDetail => 'consegnato quando il turno si chiude';

  @override
  String get composerCwdHint =>
      'pi gira nella directory dell\'host mostrata sopra.';

  @override
  String get composerConnecting => 'Connessione al server…';

  @override
  String get composerReconnecting =>
      'Riconnessione: il messaggio parte appena torna il collegamento';

  @override
  String get slashCompact => 'Compatta il contesto ora';

  @override
  String get slashClear => 'Inizia un contesto nuovo';

  @override
  String get slashModel => 'Cambia modello';

  @override
  String get slashSkills => 'Esegui una skill';

  @override
  String get slashTemplates => 'Inserisci un template di prompt';

  @override
  String get abort => 'Interrompi';

  @override
  String get agentWorking => 'L\'agente sta lavorando…';

  @override
  String childExited(String code, String when) {
    return 'Il processo è uscito con codice $code · $when';
  }

  @override
  String get compactContext => 'Compatta il contesto';

  @override
  String get copyWorkingDirectory => 'Copia la directory di lavoro';

  @override
  String get gitMenu => 'Git…';

  @override
  String get terminalMenu => 'Terminale…';

  @override
  String queueWaiting(String count) {
    return '$count messaggi in attesa';
  }

  @override
  String get queueWaitingOne => 'Un messaggio in attesa';

  @override
  String get queueClear => 'Svuota';

  @override
  String get queueSteer => 'guida';

  @override
  String get queueFollowUp => 'follow-up';

  @override
  String get dialogDeny => 'Rifiuta';

  @override
  String get dialogApprove => 'Approva';

  @override
  String get dialogSave => 'Salva';

  @override
  String get dialogCancel => 'Annulla il dialogo';

  @override
  String get dialogAnswerHint => 'Scrivi una risposta';

  @override
  String get dialogSend => 'Invia';

  @override
  String get gitTitle => 'Git';

  @override
  String get gitNothingToCommit =>
      'Nulla da committare: l\'albero di lavoro è pulito.';

  @override
  String get gitSelectChange => 'Scegli una modifica per vederne il diff.';

  @override
  String get gitCommitMessage => 'Messaggio di commit';

  @override
  String get gitCommitHint => 'cosa è cambiato';

  @override
  String get gitCommit => 'Committa';

  @override
  String get gitCommitNeedsMessage => 'Un commit vuole un messaggio.';

  @override
  String get gitStage => 'Metti in stage questo percorso';

  @override
  String gitReadFailed(String error) {
    return 'Non riesco a leggere il repository: $error';
  }

  @override
  String gitDiffFailed(String error) {
    return 'Non riesco a leggere il diff: $error';
  }

  @override
  String get gitNoTextualChange => 'Nessuna modifica testuale da mostrare.';

  @override
  String get gitWriteDisabled =>
      'La scrittura è disattivata su questo server: un admin deve abilitare \"git.write\" nelle impostazioni.';

  @override
  String actionLater(String action) {
    return '\"$action\" arriva in un passo successivo';
  }

  @override
  String get connectTitle => 'Connettiti a un server';

  @override
  String get connectIntro =>
      'Punta l\'app al server che esegue le tue sessioni pi.';

  @override
  String get serverUrlLabel => 'URL del server';

  @override
  String get serverUrlHint => 'http://pi-ui.local:8787';

  @override
  String get serverReachable => 'raggiungibile';

  @override
  String get serverNotOk => 'Il server ha risposto, ma non con \"ok\".';

  @override
  String get testConnection => 'Prova la connessione';

  @override
  String get continueToPairing => 'Continua con l\'accoppiamento';

  @override
  String get connectHelp =>
      'Il server ha bisogno di un codice di accoppiamento o della password admin: esegui `pi-ui pair` per generarne uno. Le chiavi dei provider non lasciano mai il server.';

  @override
  String get pairTitle => 'Accoppia con il server';

  @override
  String get pairInvitationNote =>
      'L\'invito è monouso e scade dopo dieci minuti.';

  @override
  String get pairDeviceNameLabel => 'Nome del dispositivo';

  @override
  String get pairCodeLabel => 'Codice di accoppiamento';

  @override
  String get pairCodeHint => '4K9M27';

  @override
  String get pairPasswordLabel => 'Password admin';

  @override
  String get pairSubmit => 'Accoppia';

  @override
  String get pairUseCode => 'Usa un codice di accoppiamento';

  @override
  String get pairUsePassword => 'Accoppia con la password admin';

  @override
  String get pairAdminNote =>
      'Il ramo password crea un dispositivo admin: gestisce dispositivi, impostazioni, MCP e aggiornamenti.';

  @override
  String get pairOperatorNote =>
      'L\'accoppiamento con codice concede operator: guida le sessioni ma non gestisce il server.';

  @override
  String get pairWrongCodeHint =>
      'Il codice può essere consumato, scaduto o sbagliato.';

  @override
  String get pairDeviceNameRequired => 'Dai un nome a questo dispositivo.';

  @override
  String get pairPasswordRequired => 'Scrivi la password admin.';

  @override
  String get pairCodeRequired => 'Scrivi il codice che il server ha stampato.';

  @override
  String get fingerprintTitle => 'Fidarsi di questo certificato?';

  @override
  String get fingerprintLabel => 'Fingerprint SHA-256';

  @override
  String get fingerprintNote =>
      'Confrontalo con `pi-ui tls fingerprint` sul server. Se cambia, l\'app rifiuta di connettersi invece di fidarsi di nuovo.';

  @override
  String get fingerprintTrust => 'Fidati e continua';

  @override
  String get terminalTitle => 'Terminale';

  @override
  String get terminalNoTerminal => 'Nessun terminale';

  @override
  String get terminalClear => 'Svuota lo scrollback';

  @override
  String get terminalDropped => 'L\'output più vecchio è stato scartato.';

  @override
  String get terminalReady => 'La shell è pronta. Scrivi un comando.';

  @override
  String get terminalHint => 'un comando, poi invio';

  @override
  String get terminalSend => 'Invia';

  @override
  String get terminalClosed => 'chiuso';

  @override
  String get chatEmptyMessage =>
      'Invia un prompt per iniziare la conversazione.';

  @override
  String get chatEmptyTitle => 'Ancora nessun messaggio';
}
