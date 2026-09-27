// ignore: unused_import
import 'package:intl/intl.dart' as intl;

import 'app_localizations.dart';

// ignore_for_file: type=lint

/// The translations for English (`en`).
class AppLocalizationsEn extends AppLocalizations {
  AppLocalizationsEn([String locale = 'en']) : super(locale);

  @override
  String get appTitle => 'pi-ui';

  @override
  String get navSessions => 'Sessions';

  @override
  String get navFiles => 'Files';

  @override
  String get navSearch => 'Search';

  @override
  String get navSettings => 'Settings';

  @override
  String get retry => 'Retry';

  @override
  String get reload => 'Reload';

  @override
  String get cancel => 'Cancel';

  @override
  String get close => 'Close';

  @override
  String get save => 'Save';

  @override
  String get send => 'Send';

  @override
  String get delete => 'Delete';

  @override
  String get refresh => 'Refresh';

  @override
  String get search => 'Search';

  @override
  String get selectASession => 'Select a session';

  @override
  String get pickASessionHint =>
      'Pick a session on the left, or start a new one.';

  @override
  String get noSessionsTitle => 'No sessions yet';

  @override
  String get noSessionsMessage =>
      'A session is one pi process in one working directory.';

  @override
  String get selectAFile => 'Select a file';

  @override
  String get pickAFileHint => 'Pick a file on the left to read it here.';

  @override
  String get noWorkspaceTitle => 'No workspace';

  @override
  String get noWorkspaceMessage => 'This server has no workspace to browse.';

  @override
  String get noWorkspaceFlagHint =>
      'The server was started without --root, so it exposes no filesystem at all.';

  @override
  String get emptyDirectoryTitle => 'Empty directory';

  @override
  String get emptyDirectoryMessage => 'Nothing in here.';

  @override
  String get directoryFailedTitle => 'That directory cannot be listed';

  @override
  String get fileFailedTitle => 'That file cannot be read';

  @override
  String get binaryFileTitle => 'Binary file';

  @override
  String get reloadFromHost => 'Reload from the host';

  @override
  String get filesTitle => 'Files';

  @override
  String get searchTitle => 'Search';

  @override
  String get searchHint => 'a word, a path or a message';

  @override
  String get searchScopeFiles => 'files';

  @override
  String get searchScopeMessages => 'messages';

  @override
  String get searchNow => 'Search now';

  @override
  String get searchIntroTitle => 'Search the host';

  @override
  String get searchIntroMessage =>
      'Type at least two characters. Files come from the workspaces, messages from the pi sessions the server was pointed at.';

  @override
  String get searchNoMatchTitle => 'No match';

  @override
  String get searchNoMatchMessage => 'Nothing in those scopes matches.';

  @override
  String get searchFailedTitle => 'The search failed';

  @override
  String get settingsTitle => 'Settings';

  @override
  String get settingsDeviceSection => 'This device';

  @override
  String get settingsPolicySection => 'Server policy';

  @override
  String get settingsUpdatesSection => 'Updates';

  @override
  String get settingsServerLabel => 'Server';

  @override
  String get settingsDeviceLabel => 'Device';

  @override
  String get settingsServerVersionLabel => 'Server version';

  @override
  String get settingsPiVersionLabel => 'pi version';

  @override
  String get settingsFeaturesLabel => 'Features';

  @override
  String get settingsCertificateLabel => 'Certificate';

  @override
  String get settingsConnectionLabel => 'Connection';

  @override
  String get settingsNotPaired => 'not paired';

  @override
  String get settingsUnknown => 'unknown';

  @override
  String get settingsKeystoreNote =>
      'The device token lives in the operating system keystore; the server keeps only its hash.';

  @override
  String get settingsNoSettings =>
      'This server has no settings: it was started without a state directory.';

  @override
  String settingsAdminHint(String scope) {
    return 'Changing these needs an admin device; this one is a $scope.';
  }

  @override
  String get settingsChanged => 'changed';

  @override
  String get settingsResetTooltip => 'Back to the default';

  @override
  String settingsReadFailed(String error) {
    return 'The settings could not be read: $error';
  }

  @override
  String get updatesManagedNote =>
      'This deployment manages its own updates: the server runs no script of its own.';

  @override
  String get updatesApply => 'Run the update';

  @override
  String updatesApplyStarted(String id) {
    return 'Update started: task $id. Its output is on the tasks endpoint.';
  }

  @override
  String get updatesApplyStartedNoId => 'Update started.';

  @override
  String updatesInstalledLatest(String current, String latest) {
    return 'installed $current · latest $latest';
  }

  @override
  String updatesInstalledUnknown(String current, String error) {
    return 'installed $current · not checked ($error)';
  }

  @override
  String updatesChecked(String when) {
    return 'checked $when';
  }

  @override
  String get updatesAvailable => 'update';

  @override
  String updatesReadFailed(String error) {
    return 'The versions could not be read: $error';
  }

  @override
  String get connectionConnecting => 'Connecting to the server…';

  @override
  String get connectionLost => 'Connection lost: retrying…';

  @override
  String get connectionIdle => 'Not connected.';

  @override
  String get connectionRetry => 'Retry';

  @override
  String get sessionsTitle => 'Sessions';

  @override
  String get newSession => 'New session';

  @override
  String get newSessionHint =>
      'pi runs in a host directory; the client never reads provider secrets.';

  @override
  String get workingDirectory => 'Working directory';

  @override
  String get workingDirectoryHint => '/home/user/Projects/my-project';

  @override
  String get workingDirectoryRequired => 'A working directory is required.';

  @override
  String get nameOptional => 'Name (optional)';

  @override
  String get nameHint => 'pi-ui';

  @override
  String get create => 'Create';

  @override
  String get newSessionTooltip => 'New session';

  @override
  String get stopSession => 'Stop session';

  @override
  String get stopSessionTitle => 'Stop this session?';

  @override
  String stopSessionMessage(String cwd) {
    return 'pi in $cwd is asked to shut down. The conversation stays on disk and the session can be resumed later.';
  }

  @override
  String get stopSessionConfirm => 'Stop';

  @override
  String get sessionsFailedTitle => 'Could not load the sessions';

  @override
  String get noEventsYet => 'no events yet';

  @override
  String messagesCount(String count) {
    return '$count msgs';
  }

  @override
  String queuedCount(String count) {
    return '$count queued';
  }

  @override
  String get sessionActions => 'Session actions';

  @override
  String get rename => 'Rename…';

  @override
  String get renameTitle => 'Rename the session';

  @override
  String get clone => 'Clone';

  @override
  String get export => 'Export…';

  @override
  String get removeFromList => 'Remove from list';

  @override
  String get chatNotConnected => 'Not connected to the server.';

  @override
  String get composerHintIdle => 'Message pi… (/ for commands)';

  @override
  String get composerHintStreaming => 'Steer the run or leave a follow-up';

  @override
  String get composerHintOffline =>
      'Reconnecting — the message waits, it is not lost';

  @override
  String get composerSend => 'Send';

  @override
  String get composerSteer => 'Steer';

  @override
  String get composerFollowUp => 'Follow-up';

  @override
  String get composerQueue => 'Queue';

  @override
  String get composerChooseMode => 'Choose steer or follow-up';

  @override
  String get composerSteeringHint => 'Session is streaming';

  @override
  String get composerSteerDetail => 'delivered after the current tool calls';

  @override
  String get composerFollowUpDetail => 'delivered when the run settles';

  @override
  String get composerCwdHint => 'pi runs in the host directory shown above.';

  @override
  String get composerConnecting => 'Connecting to the server…';

  @override
  String get composerReconnecting =>
      'Reconnecting: the message is sent when the link is back';

  @override
  String get slashCompact => 'Compact the context now';

  @override
  String get slashClear => 'Start a fresh context';

  @override
  String get slashModel => 'Switch the model';

  @override
  String get slashSkills => 'Run a skill';

  @override
  String get slashTemplates => 'Insert a prompt template';

  @override
  String get abort => 'Abort';

  @override
  String get agentWorking => 'The agent is working…';

  @override
  String childExited(String code, String when) {
    return 'Child exited with code $code · $when';
  }

  @override
  String get compactContext => 'Compact context';

  @override
  String get copyWorkingDirectory => 'Copy the working directory';

  @override
  String get gitMenu => 'Git…';

  @override
  String get terminalMenu => 'Terminal…';

  @override
  String queueWaiting(String count) {
    return '$count messages waiting';
  }

  @override
  String get queueWaitingOne => 'One message waiting';

  @override
  String get queueClear => 'Clear';

  @override
  String get queueSteer => 'steer';

  @override
  String get queueFollowUp => 'follow-up';

  @override
  String get dialogDeny => 'Deny';

  @override
  String get dialogApprove => 'Approve';

  @override
  String get dialogSave => 'Save';

  @override
  String get dialogCancel => 'Cancel the dialog';

  @override
  String get dialogAnswerHint => 'Type an answer';

  @override
  String get dialogSend => 'Send';

  @override
  String get gitTitle => 'Git';

  @override
  String get gitNothingToCommit =>
      'Nothing to commit: the working tree is clean.';

  @override
  String get gitSelectChange => 'Select a change to see its diff.';

  @override
  String get gitCommitMessage => 'Commit message';

  @override
  String get gitCommitHint => 'what changed';

  @override
  String get gitCommit => 'Commit';

  @override
  String get gitCommitNeedsMessage => 'A commit needs a message.';

  @override
  String get gitStage => 'Stage this path';

  @override
  String gitReadFailed(String error) {
    return 'The repository could not be read: $error';
  }

  @override
  String gitDiffFailed(String error) {
    return 'The diff could not be read: $error';
  }

  @override
  String get gitNoTextualChange => 'No textual change to show.';

  @override
  String get gitWriteDisabled =>
      'Writing is turned off on this server: an admin has to enable \"git.write\" in the settings.';

  @override
  String actionLater(String action) {
    return '\"$action\" lands in a later slice';
  }

  @override
  String get connectTitle => 'Connect to a server';

  @override
  String get connectIntro =>
      'Point the app at the server that runs your pi sessions.';

  @override
  String get serverUrlLabel => 'Server URL';

  @override
  String get serverUrlHint => 'http://pi-ui.local:8787';

  @override
  String get serverReachable => 'reachable';

  @override
  String get serverNotOk => 'The server answered, but not with \"ok\".';

  @override
  String get testConnection => 'Test connection';

  @override
  String get continueToPairing => 'Continue to pairing';

  @override
  String get connectHelp =>
      'The server needs a pairing code or your admin password: run `pi-ui pair` on it to mint one. Provider keys never leave the server.';

  @override
  String get pairTitle => 'Pair with the server';

  @override
  String get pairInvitationNote =>
      'The invitation is single use and expires after ten minutes.';

  @override
  String get pairDeviceNameLabel => 'Device name';

  @override
  String get pairCodeLabel => 'Pairing code';

  @override
  String get pairCodeHint => '4K9M27';

  @override
  String get pairPasswordLabel => 'Admin password';

  @override
  String get pairSubmit => 'Pair';

  @override
  String get pairUseCode => 'Use a pairing code instead';

  @override
  String get pairUsePassword => 'Pair with the admin password';

  @override
  String get pairAdminNote =>
      'The password branch mints an admin device: it manages devices, settings, MCP and updates.';

  @override
  String get pairOperatorNote =>
      'Pairing with a code grants operator: it drives sessions but cannot manage the server.';

  @override
  String get pairWrongCodeHint => 'The code may be consumed, expired or wrong.';

  @override
  String get pairDeviceNameRequired => 'Give this device a name.';

  @override
  String get pairPasswordRequired => 'Type the admin password.';

  @override
  String get pairCodeRequired => 'Type the pairing code the server printed.';

  @override
  String get fingerprintTitle => 'Trust this certificate?';

  @override
  String get fingerprintLabel => 'SHA-256 fingerprint';

  @override
  String get fingerprintNote =>
      'Compare it with `pi-ui tls fingerprint` on the server. If it ever changes, this app refuses to connect instead of trusting it again.';

  @override
  String get fingerprintTrust => 'Trust and continue';

  @override
  String get terminalTitle => 'Terminal';

  @override
  String get terminalNoTerminal => 'No terminal';

  @override
  String get terminalClear => 'Clear the scrollback';

  @override
  String get terminalDropped => 'The oldest output was dropped.';

  @override
  String get terminalReady => 'The shell is ready. Type a command.';

  @override
  String get terminalHint => 'a command, then enter';

  @override
  String get terminalSend => 'Send';

  @override
  String get terminalClosed => 'closed';

  @override
  String get chatEmptyMessage => 'Send a prompt to start the conversation.';

  @override
  String get chatEmptyTitle => 'No messages yet';
}
