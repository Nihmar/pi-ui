import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:intl/intl.dart' as intl;

import 'app_localizations_en.dart';
import 'app_localizations_it.dart';

// ignore_for_file: type=lint

/// Callers can lookup localized strings with an instance of AppLocalizations
/// returned by `AppLocalizations.of(context)`.
///
/// Applications need to include `AppLocalizations.delegate()` in their app's
/// `localizationDelegates` list, and the locales they support in the app's
/// `supportedLocales` list. For example:
///
/// ```dart
/// import 'l10n/app_localizations.dart';
///
/// return MaterialApp(
///   localizationsDelegates: AppLocalizations.localizationsDelegates,
///   supportedLocales: AppLocalizations.supportedLocales,
///   home: MyApplicationHome(),
/// );
/// ```
///
/// ## Update pubspec.yaml
///
/// Please make sure to update your pubspec.yaml to include the following
/// packages:
///
/// ```yaml
/// dependencies:
///   # Internationalization support.
///   flutter_localizations:
///     sdk: flutter
///   intl: any # Use the pinned version from flutter_localizations
///
///   # Rest of dependencies
/// ```
///
/// ## iOS Applications
///
/// iOS applications define key application metadata, including supported
/// locales, in an Info.plist file that is built into the application bundle.
/// To configure the locales supported by your app, you’ll need to edit this
/// file.
///
/// First, open your project’s ios/Runner.xcworkspace Xcode workspace file.
/// Then, in the Project Navigator, open the Info.plist file under the Runner
/// project’s Runner folder.
///
/// Next, select the Information Property List item, select Add Item from the
/// Editor menu, then select Localizations from the pop-up menu.
///
/// Select and expand the newly-created Localizations item then, for each
/// locale your application supports, add a new item and select the locale
/// you wish to add from the pop-up menu in the Value field. This list should
/// be consistent with the languages listed in the AppLocalizations.supportedLocales
/// property.
abstract class AppLocalizations {
  AppLocalizations(String locale)
    : localeName = intl.Intl.canonicalizedLocale(locale.toString());

  final String localeName;

  static AppLocalizations of(BuildContext context) {
    return Localizations.of<AppLocalizations>(context, AppLocalizations)!;
  }

  static const LocalizationsDelegate<AppLocalizations> delegate =
      _AppLocalizationsDelegate();

  /// A list of this localizations delegate along with the default localizations
  /// delegates.
  ///
  /// Returns a list of localizations delegates containing this delegate along with
  /// GlobalMaterialLocalizations.delegate, GlobalCupertinoLocalizations.delegate,
  /// and GlobalWidgetsLocalizations.delegate.
  ///
  /// Additional delegates can be added by appending to this list in
  /// MaterialApp. This list does not have to be used at all if a custom list
  /// of delegates is preferred or required.
  static const List<LocalizationsDelegate<dynamic>> localizationsDelegates =
      <LocalizationsDelegate<dynamic>>[
        delegate,
        GlobalMaterialLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
      ];

  /// A list of this localizations delegate's supported locales.
  static const List<Locale> supportedLocales = <Locale>[
    Locale('en'),
    Locale('it'),
  ];

  /// No description provided for @appTitle.
  ///
  /// In en, this message translates to:
  /// **'pi-ui'**
  String get appTitle;

  /// No description provided for @navSessions.
  ///
  /// In en, this message translates to:
  /// **'Sessions'**
  String get navSessions;

  /// No description provided for @navFiles.
  ///
  /// In en, this message translates to:
  /// **'Files'**
  String get navFiles;

  /// No description provided for @navSearch.
  ///
  /// In en, this message translates to:
  /// **'Search'**
  String get navSearch;

  /// No description provided for @navSettings.
  ///
  /// In en, this message translates to:
  /// **'Settings'**
  String get navSettings;

  /// No description provided for @retry.
  ///
  /// In en, this message translates to:
  /// **'Retry'**
  String get retry;

  /// No description provided for @reload.
  ///
  /// In en, this message translates to:
  /// **'Reload'**
  String get reload;

  /// No description provided for @cancel.
  ///
  /// In en, this message translates to:
  /// **'Cancel'**
  String get cancel;

  /// No description provided for @close.
  ///
  /// In en, this message translates to:
  /// **'Close'**
  String get close;

  /// No description provided for @save.
  ///
  /// In en, this message translates to:
  /// **'Save'**
  String get save;

  /// No description provided for @send.
  ///
  /// In en, this message translates to:
  /// **'Send'**
  String get send;

  /// No description provided for @delete.
  ///
  /// In en, this message translates to:
  /// **'Delete'**
  String get delete;

  /// No description provided for @refresh.
  ///
  /// In en, this message translates to:
  /// **'Refresh'**
  String get refresh;

  /// No description provided for @search.
  ///
  /// In en, this message translates to:
  /// **'Search'**
  String get search;

  /// No description provided for @selectASession.
  ///
  /// In en, this message translates to:
  /// **'Select a session'**
  String get selectASession;

  /// No description provided for @pickASessionHint.
  ///
  /// In en, this message translates to:
  /// **'Pick a session on the left, or start a new one.'**
  String get pickASessionHint;

  /// No description provided for @noSessionsTitle.
  ///
  /// In en, this message translates to:
  /// **'No sessions yet'**
  String get noSessionsTitle;

  /// No description provided for @noSessionsMessage.
  ///
  /// In en, this message translates to:
  /// **'A session is one pi process in one working directory.'**
  String get noSessionsMessage;

  /// No description provided for @selectAFile.
  ///
  /// In en, this message translates to:
  /// **'Select a file'**
  String get selectAFile;

  /// No description provided for @pickAFileHint.
  ///
  /// In en, this message translates to:
  /// **'Pick a file on the left to read it here.'**
  String get pickAFileHint;

  /// No description provided for @noWorkspaceTitle.
  ///
  /// In en, this message translates to:
  /// **'No workspace'**
  String get noWorkspaceTitle;

  /// No description provided for @noWorkspaceMessage.
  ///
  /// In en, this message translates to:
  /// **'This server has no workspace to browse.'**
  String get noWorkspaceMessage;

  /// No description provided for @noWorkspaceFlagHint.
  ///
  /// In en, this message translates to:
  /// **'The server was started without --root, so it exposes no filesystem at all.'**
  String get noWorkspaceFlagHint;

  /// No description provided for @emptyDirectoryTitle.
  ///
  /// In en, this message translates to:
  /// **'Empty directory'**
  String get emptyDirectoryTitle;

  /// No description provided for @emptyDirectoryMessage.
  ///
  /// In en, this message translates to:
  /// **'Nothing in here.'**
  String get emptyDirectoryMessage;

  /// No description provided for @directoryFailedTitle.
  ///
  /// In en, this message translates to:
  /// **'That directory cannot be listed'**
  String get directoryFailedTitle;

  /// No description provided for @fileFailedTitle.
  ///
  /// In en, this message translates to:
  /// **'That file cannot be read'**
  String get fileFailedTitle;

  /// No description provided for @binaryFileTitle.
  ///
  /// In en, this message translates to:
  /// **'Binary file'**
  String get binaryFileTitle;

  /// No description provided for @reloadFromHost.
  ///
  /// In en, this message translates to:
  /// **'Reload from the host'**
  String get reloadFromHost;

  /// No description provided for @filesTitle.
  ///
  /// In en, this message translates to:
  /// **'Files'**
  String get filesTitle;

  /// No description provided for @searchTitle.
  ///
  /// In en, this message translates to:
  /// **'Search'**
  String get searchTitle;

  /// No description provided for @searchHint.
  ///
  /// In en, this message translates to:
  /// **'a word, a path or a message'**
  String get searchHint;

  /// No description provided for @searchScopeFiles.
  ///
  /// In en, this message translates to:
  /// **'files'**
  String get searchScopeFiles;

  /// No description provided for @searchScopeMessages.
  ///
  /// In en, this message translates to:
  /// **'messages'**
  String get searchScopeMessages;

  /// No description provided for @searchNow.
  ///
  /// In en, this message translates to:
  /// **'Search now'**
  String get searchNow;

  /// No description provided for @searchIntroTitle.
  ///
  /// In en, this message translates to:
  /// **'Search the host'**
  String get searchIntroTitle;

  /// No description provided for @searchIntroMessage.
  ///
  /// In en, this message translates to:
  /// **'Type at least two characters. Files come from the workspaces, messages from the pi sessions the server was pointed at.'**
  String get searchIntroMessage;

  /// No description provided for @searchNoMatchTitle.
  ///
  /// In en, this message translates to:
  /// **'No match'**
  String get searchNoMatchTitle;

  /// No description provided for @searchNoMatchMessage.
  ///
  /// In en, this message translates to:
  /// **'Nothing in those scopes matches.'**
  String get searchNoMatchMessage;

  /// No description provided for @searchFailedTitle.
  ///
  /// In en, this message translates to:
  /// **'The search failed'**
  String get searchFailedTitle;

  /// No description provided for @settingsTitle.
  ///
  /// In en, this message translates to:
  /// **'Settings'**
  String get settingsTitle;

  /// No description provided for @settingsDeviceSection.
  ///
  /// In en, this message translates to:
  /// **'This device'**
  String get settingsDeviceSection;

  /// No description provided for @settingsPolicySection.
  ///
  /// In en, this message translates to:
  /// **'Server policy'**
  String get settingsPolicySection;

  /// No description provided for @settingsUpdatesSection.
  ///
  /// In en, this message translates to:
  /// **'Updates'**
  String get settingsUpdatesSection;

  /// No description provided for @settingsServerLabel.
  ///
  /// In en, this message translates to:
  /// **'Server'**
  String get settingsServerLabel;

  /// No description provided for @settingsDeviceLabel.
  ///
  /// In en, this message translates to:
  /// **'Device'**
  String get settingsDeviceLabel;

  /// No description provided for @settingsServerVersionLabel.
  ///
  /// In en, this message translates to:
  /// **'Server version'**
  String get settingsServerVersionLabel;

  /// No description provided for @settingsPiVersionLabel.
  ///
  /// In en, this message translates to:
  /// **'pi version'**
  String get settingsPiVersionLabel;

  /// No description provided for @settingsFeaturesLabel.
  ///
  /// In en, this message translates to:
  /// **'Features'**
  String get settingsFeaturesLabel;

  /// No description provided for @settingsCertificateLabel.
  ///
  /// In en, this message translates to:
  /// **'Certificate'**
  String get settingsCertificateLabel;

  /// No description provided for @settingsConnectionLabel.
  ///
  /// In en, this message translates to:
  /// **'Connection'**
  String get settingsConnectionLabel;

  /// No description provided for @settingsNotPaired.
  ///
  /// In en, this message translates to:
  /// **'not paired'**
  String get settingsNotPaired;

  /// No description provided for @settingsUnknown.
  ///
  /// In en, this message translates to:
  /// **'unknown'**
  String get settingsUnknown;

  /// No description provided for @settingsKeystoreNote.
  ///
  /// In en, this message translates to:
  /// **'The device token lives in the operating system keystore; the server keeps only its hash.'**
  String get settingsKeystoreNote;

  /// No description provided for @settingsNoSettings.
  ///
  /// In en, this message translates to:
  /// **'This server has no settings: it was started without a state directory.'**
  String get settingsNoSettings;

  /// No description provided for @settingsAdminHint.
  ///
  /// In en, this message translates to:
  /// **'Changing these needs an admin device; this one is a {scope}.'**
  String settingsAdminHint(String scope);

  /// No description provided for @settingsChanged.
  ///
  /// In en, this message translates to:
  /// **'changed'**
  String get settingsChanged;

  /// No description provided for @settingsResetTooltip.
  ///
  /// In en, this message translates to:
  /// **'Back to the default'**
  String get settingsResetTooltip;

  /// No description provided for @settingsReadFailed.
  ///
  /// In en, this message translates to:
  /// **'The settings could not be read: {error}'**
  String settingsReadFailed(String error);

  /// No description provided for @updatesManagedNote.
  ///
  /// In en, this message translates to:
  /// **'This deployment manages its own updates: the server runs no script of its own.'**
  String get updatesManagedNote;

  /// No description provided for @updatesApply.
  ///
  /// In en, this message translates to:
  /// **'Run the update'**
  String get updatesApply;

  /// No description provided for @updatesApplyStarted.
  ///
  /// In en, this message translates to:
  /// **'Update started: task {id}. Its output is on the tasks endpoint.'**
  String updatesApplyStarted(String id);

  /// No description provided for @updatesApplyStartedNoId.
  ///
  /// In en, this message translates to:
  /// **'Update started.'**
  String get updatesApplyStartedNoId;

  /// No description provided for @updatesInstalledLatest.
  ///
  /// In en, this message translates to:
  /// **'installed {current} · latest {latest}'**
  String updatesInstalledLatest(String current, String latest);

  /// No description provided for @updatesInstalledUnknown.
  ///
  /// In en, this message translates to:
  /// **'installed {current} · not checked ({error})'**
  String updatesInstalledUnknown(String current, String error);

  /// No description provided for @updatesChecked.
  ///
  /// In en, this message translates to:
  /// **'checked {when}'**
  String updatesChecked(String when);

  /// No description provided for @updatesAvailable.
  ///
  /// In en, this message translates to:
  /// **'update'**
  String get updatesAvailable;

  /// No description provided for @updatesReadFailed.
  ///
  /// In en, this message translates to:
  /// **'The versions could not be read: {error}'**
  String updatesReadFailed(String error);

  /// No description provided for @connectionConnecting.
  ///
  /// In en, this message translates to:
  /// **'Connecting to the server…'**
  String get connectionConnecting;

  /// No description provided for @connectionLost.
  ///
  /// In en, this message translates to:
  /// **'Connection lost: retrying…'**
  String get connectionLost;

  /// No description provided for @connectionIdle.
  ///
  /// In en, this message translates to:
  /// **'Not connected.'**
  String get connectionIdle;

  /// No description provided for @connectionRetry.
  ///
  /// In en, this message translates to:
  /// **'Retry'**
  String get connectionRetry;

  /// No description provided for @sessionsTitle.
  ///
  /// In en, this message translates to:
  /// **'Sessions'**
  String get sessionsTitle;

  /// No description provided for @newSession.
  ///
  /// In en, this message translates to:
  /// **'New session'**
  String get newSession;

  /// No description provided for @newSessionHint.
  ///
  /// In en, this message translates to:
  /// **'pi runs in a host directory; the client never reads provider secrets.'**
  String get newSessionHint;

  /// No description provided for @workingDirectory.
  ///
  /// In en, this message translates to:
  /// **'Working directory'**
  String get workingDirectory;

  /// No description provided for @workingDirectoryHint.
  ///
  /// In en, this message translates to:
  /// **'/home/user/Projects/my-project'**
  String get workingDirectoryHint;

  /// No description provided for @workingDirectoryRequired.
  ///
  /// In en, this message translates to:
  /// **'A working directory is required.'**
  String get workingDirectoryRequired;

  /// No description provided for @nameOptional.
  ///
  /// In en, this message translates to:
  /// **'Name (optional)'**
  String get nameOptional;

  /// No description provided for @nameHint.
  ///
  /// In en, this message translates to:
  /// **'pi-ui'**
  String get nameHint;

  /// No description provided for @create.
  ///
  /// In en, this message translates to:
  /// **'Create'**
  String get create;

  /// No description provided for @newSessionTooltip.
  ///
  /// In en, this message translates to:
  /// **'New session'**
  String get newSessionTooltip;

  /// No description provided for @stopSession.
  ///
  /// In en, this message translates to:
  /// **'Stop session'**
  String get stopSession;

  /// No description provided for @stopSessionTitle.
  ///
  /// In en, this message translates to:
  /// **'Stop this session?'**
  String get stopSessionTitle;

  /// No description provided for @stopSessionMessage.
  ///
  /// In en, this message translates to:
  /// **'pi in {cwd} is asked to shut down. The conversation stays on disk and the session can be resumed later.'**
  String stopSessionMessage(String cwd);

  /// No description provided for @stopSessionConfirm.
  ///
  /// In en, this message translates to:
  /// **'Stop'**
  String get stopSessionConfirm;

  /// No description provided for @sessionsFailedTitle.
  ///
  /// In en, this message translates to:
  /// **'Could not load the sessions'**
  String get sessionsFailedTitle;

  /// No description provided for @noEventsYet.
  ///
  /// In en, this message translates to:
  /// **'no events yet'**
  String get noEventsYet;

  /// No description provided for @messagesCount.
  ///
  /// In en, this message translates to:
  /// **'{count} msgs'**
  String messagesCount(String count);

  /// No description provided for @queuedCount.
  ///
  /// In en, this message translates to:
  /// **'{count} queued'**
  String queuedCount(String count);

  /// No description provided for @sessionActions.
  ///
  /// In en, this message translates to:
  /// **'Session actions'**
  String get sessionActions;

  /// No description provided for @rename.
  ///
  /// In en, this message translates to:
  /// **'Rename…'**
  String get rename;

  /// No description provided for @renameTitle.
  ///
  /// In en, this message translates to:
  /// **'Rename the session'**
  String get renameTitle;

  /// No description provided for @clone.
  ///
  /// In en, this message translates to:
  /// **'Clone'**
  String get clone;

  /// No description provided for @export.
  ///
  /// In en, this message translates to:
  /// **'Export…'**
  String get export;

  /// No description provided for @removeFromList.
  ///
  /// In en, this message translates to:
  /// **'Remove from list'**
  String get removeFromList;

  /// No description provided for @chatNotConnected.
  ///
  /// In en, this message translates to:
  /// **'Not connected to the server.'**
  String get chatNotConnected;

  /// No description provided for @composerHintIdle.
  ///
  /// In en, this message translates to:
  /// **'Message pi… (/ for commands)'**
  String get composerHintIdle;

  /// No description provided for @composerHintStreaming.
  ///
  /// In en, this message translates to:
  /// **'Steer the run or leave a follow-up'**
  String get composerHintStreaming;

  /// No description provided for @composerHintOffline.
  ///
  /// In en, this message translates to:
  /// **'Reconnecting — the message waits, it is not lost'**
  String get composerHintOffline;

  /// No description provided for @composerSend.
  ///
  /// In en, this message translates to:
  /// **'Send'**
  String get composerSend;

  /// No description provided for @composerSteer.
  ///
  /// In en, this message translates to:
  /// **'Steer'**
  String get composerSteer;

  /// No description provided for @composerFollowUp.
  ///
  /// In en, this message translates to:
  /// **'Follow-up'**
  String get composerFollowUp;

  /// No description provided for @composerQueue.
  ///
  /// In en, this message translates to:
  /// **'Queue'**
  String get composerQueue;

  /// No description provided for @composerChooseMode.
  ///
  /// In en, this message translates to:
  /// **'Choose steer or follow-up'**
  String get composerChooseMode;

  /// No description provided for @composerSteeringHint.
  ///
  /// In en, this message translates to:
  /// **'Session is streaming'**
  String get composerSteeringHint;

  /// No description provided for @composerSteerDetail.
  ///
  /// In en, this message translates to:
  /// **'delivered after the current tool calls'**
  String get composerSteerDetail;

  /// No description provided for @composerFollowUpDetail.
  ///
  /// In en, this message translates to:
  /// **'delivered when the run settles'**
  String get composerFollowUpDetail;

  /// No description provided for @composerCwdHint.
  ///
  /// In en, this message translates to:
  /// **'pi runs in the host directory shown above.'**
  String get composerCwdHint;

  /// No description provided for @composerConnecting.
  ///
  /// In en, this message translates to:
  /// **'Connecting to the server…'**
  String get composerConnecting;

  /// No description provided for @composerReconnecting.
  ///
  /// In en, this message translates to:
  /// **'Reconnecting: the message is sent when the link is back'**
  String get composerReconnecting;

  /// No description provided for @slashCompact.
  ///
  /// In en, this message translates to:
  /// **'Compact the context now'**
  String get slashCompact;

  /// No description provided for @slashClear.
  ///
  /// In en, this message translates to:
  /// **'Start a fresh context'**
  String get slashClear;

  /// No description provided for @slashModel.
  ///
  /// In en, this message translates to:
  /// **'Switch the model'**
  String get slashModel;

  /// No description provided for @slashSkills.
  ///
  /// In en, this message translates to:
  /// **'Run a skill'**
  String get slashSkills;

  /// No description provided for @slashTemplates.
  ///
  /// In en, this message translates to:
  /// **'Insert a prompt template'**
  String get slashTemplates;

  /// No description provided for @abort.
  ///
  /// In en, this message translates to:
  /// **'Abort'**
  String get abort;

  /// No description provided for @agentWorking.
  ///
  /// In en, this message translates to:
  /// **'The agent is working…'**
  String get agentWorking;

  /// No description provided for @childExited.
  ///
  /// In en, this message translates to:
  /// **'Child exited with code {code} · {when}'**
  String childExited(String code, String when);

  /// No description provided for @compactContext.
  ///
  /// In en, this message translates to:
  /// **'Compact context'**
  String get compactContext;

  /// No description provided for @copyWorkingDirectory.
  ///
  /// In en, this message translates to:
  /// **'Copy the working directory'**
  String get copyWorkingDirectory;

  /// No description provided for @gitMenu.
  ///
  /// In en, this message translates to:
  /// **'Git…'**
  String get gitMenu;

  /// No description provided for @terminalMenu.
  ///
  /// In en, this message translates to:
  /// **'Terminal…'**
  String get terminalMenu;

  /// No description provided for @queueWaiting.
  ///
  /// In en, this message translates to:
  /// **'{count} messages waiting'**
  String queueWaiting(String count);

  /// No description provided for @queueWaitingOne.
  ///
  /// In en, this message translates to:
  /// **'One message waiting'**
  String get queueWaitingOne;

  /// No description provided for @queueClear.
  ///
  /// In en, this message translates to:
  /// **'Clear'**
  String get queueClear;

  /// No description provided for @queueSteer.
  ///
  /// In en, this message translates to:
  /// **'steer'**
  String get queueSteer;

  /// No description provided for @queueFollowUp.
  ///
  /// In en, this message translates to:
  /// **'follow-up'**
  String get queueFollowUp;

  /// No description provided for @dialogDeny.
  ///
  /// In en, this message translates to:
  /// **'Deny'**
  String get dialogDeny;

  /// No description provided for @dialogApprove.
  ///
  /// In en, this message translates to:
  /// **'Approve'**
  String get dialogApprove;

  /// No description provided for @dialogSave.
  ///
  /// In en, this message translates to:
  /// **'Save'**
  String get dialogSave;

  /// No description provided for @dialogCancel.
  ///
  /// In en, this message translates to:
  /// **'Cancel the dialog'**
  String get dialogCancel;

  /// No description provided for @dialogAnswerHint.
  ///
  /// In en, this message translates to:
  /// **'Type an answer'**
  String get dialogAnswerHint;

  /// No description provided for @dialogSend.
  ///
  /// In en, this message translates to:
  /// **'Send'**
  String get dialogSend;

  /// No description provided for @gitTitle.
  ///
  /// In en, this message translates to:
  /// **'Git'**
  String get gitTitle;

  /// No description provided for @gitNothingToCommit.
  ///
  /// In en, this message translates to:
  /// **'Nothing to commit: the working tree is clean.'**
  String get gitNothingToCommit;

  /// No description provided for @gitSelectChange.
  ///
  /// In en, this message translates to:
  /// **'Select a change to see its diff.'**
  String get gitSelectChange;

  /// No description provided for @gitCommitMessage.
  ///
  /// In en, this message translates to:
  /// **'Commit message'**
  String get gitCommitMessage;

  /// No description provided for @gitCommitHint.
  ///
  /// In en, this message translates to:
  /// **'what changed'**
  String get gitCommitHint;

  /// No description provided for @gitCommit.
  ///
  /// In en, this message translates to:
  /// **'Commit'**
  String get gitCommit;

  /// No description provided for @gitCommitNeedsMessage.
  ///
  /// In en, this message translates to:
  /// **'A commit needs a message.'**
  String get gitCommitNeedsMessage;

  /// No description provided for @gitStage.
  ///
  /// In en, this message translates to:
  /// **'Stage this path'**
  String get gitStage;

  /// No description provided for @gitReadFailed.
  ///
  /// In en, this message translates to:
  /// **'The repository could not be read: {error}'**
  String gitReadFailed(String error);

  /// No description provided for @gitDiffFailed.
  ///
  /// In en, this message translates to:
  /// **'The diff could not be read: {error}'**
  String gitDiffFailed(String error);

  /// No description provided for @gitNoTextualChange.
  ///
  /// In en, this message translates to:
  /// **'No textual change to show.'**
  String get gitNoTextualChange;

  /// No description provided for @gitWriteDisabled.
  ///
  /// In en, this message translates to:
  /// **'Writing is turned off on this server: an admin has to enable \"git.write\" in the settings.'**
  String get gitWriteDisabled;

  /// No description provided for @actionLater.
  ///
  /// In en, this message translates to:
  /// **'\"{action}\" lands in a later slice'**
  String actionLater(String action);

  /// No description provided for @connectTitle.
  ///
  /// In en, this message translates to:
  /// **'Connect to a server'**
  String get connectTitle;

  /// No description provided for @connectIntro.
  ///
  /// In en, this message translates to:
  /// **'Point the app at the server that runs your pi sessions.'**
  String get connectIntro;

  /// No description provided for @serverUrlLabel.
  ///
  /// In en, this message translates to:
  /// **'Server URL'**
  String get serverUrlLabel;

  /// No description provided for @serverUrlHint.
  ///
  /// In en, this message translates to:
  /// **'http://pi-ui.local:8787'**
  String get serverUrlHint;

  /// No description provided for @serverReachable.
  ///
  /// In en, this message translates to:
  /// **'reachable'**
  String get serverReachable;

  /// No description provided for @serverNotOk.
  ///
  /// In en, this message translates to:
  /// **'The server answered, but not with \"ok\".'**
  String get serverNotOk;

  /// No description provided for @testConnection.
  ///
  /// In en, this message translates to:
  /// **'Test connection'**
  String get testConnection;

  /// No description provided for @continueToPairing.
  ///
  /// In en, this message translates to:
  /// **'Continue to pairing'**
  String get continueToPairing;

  /// No description provided for @connectHelp.
  ///
  /// In en, this message translates to:
  /// **'The server needs a pairing code or your admin password: run `pi-ui pair` on it to mint one. Provider keys never leave the server.'**
  String get connectHelp;

  /// No description provided for @pairTitle.
  ///
  /// In en, this message translates to:
  /// **'Pair with the server'**
  String get pairTitle;

  /// No description provided for @pairInvitationNote.
  ///
  /// In en, this message translates to:
  /// **'The invitation is single use and expires after ten minutes.'**
  String get pairInvitationNote;

  /// No description provided for @pairDeviceNameLabel.
  ///
  /// In en, this message translates to:
  /// **'Device name'**
  String get pairDeviceNameLabel;

  /// No description provided for @pairCodeLabel.
  ///
  /// In en, this message translates to:
  /// **'Pairing code'**
  String get pairCodeLabel;

  /// No description provided for @pairCodeHint.
  ///
  /// In en, this message translates to:
  /// **'4K9M27'**
  String get pairCodeHint;

  /// No description provided for @pairPasswordLabel.
  ///
  /// In en, this message translates to:
  /// **'Admin password'**
  String get pairPasswordLabel;

  /// No description provided for @pairSubmit.
  ///
  /// In en, this message translates to:
  /// **'Pair'**
  String get pairSubmit;

  /// No description provided for @pairUseCode.
  ///
  /// In en, this message translates to:
  /// **'Use a pairing code instead'**
  String get pairUseCode;

  /// No description provided for @pairUsePassword.
  ///
  /// In en, this message translates to:
  /// **'Pair with the admin password'**
  String get pairUsePassword;

  /// No description provided for @pairAdminNote.
  ///
  /// In en, this message translates to:
  /// **'The password branch mints an admin device: it manages devices, settings, MCP and updates.'**
  String get pairAdminNote;

  /// No description provided for @pairOperatorNote.
  ///
  /// In en, this message translates to:
  /// **'Pairing with a code grants operator: it drives sessions but cannot manage the server.'**
  String get pairOperatorNote;

  /// No description provided for @pairWrongCodeHint.
  ///
  /// In en, this message translates to:
  /// **'The code may be consumed, expired or wrong.'**
  String get pairWrongCodeHint;

  /// No description provided for @pairDeviceNameRequired.
  ///
  /// In en, this message translates to:
  /// **'Give this device a name.'**
  String get pairDeviceNameRequired;

  /// No description provided for @pairPasswordRequired.
  ///
  /// In en, this message translates to:
  /// **'Type the admin password.'**
  String get pairPasswordRequired;

  /// No description provided for @pairCodeRequired.
  ///
  /// In en, this message translates to:
  /// **'Type the pairing code the server printed.'**
  String get pairCodeRequired;

  /// No description provided for @fingerprintTitle.
  ///
  /// In en, this message translates to:
  /// **'Trust this certificate?'**
  String get fingerprintTitle;

  /// No description provided for @fingerprintLabel.
  ///
  /// In en, this message translates to:
  /// **'SHA-256 fingerprint'**
  String get fingerprintLabel;

  /// No description provided for @fingerprintNote.
  ///
  /// In en, this message translates to:
  /// **'Compare it with `pi-ui tls fingerprint` on the server. If it ever changes, this app refuses to connect instead of trusting it again.'**
  String get fingerprintNote;

  /// No description provided for @fingerprintTrust.
  ///
  /// In en, this message translates to:
  /// **'Trust and continue'**
  String get fingerprintTrust;

  /// No description provided for @terminalTitle.
  ///
  /// In en, this message translates to:
  /// **'Terminal'**
  String get terminalTitle;

  /// No description provided for @terminalNoTerminal.
  ///
  /// In en, this message translates to:
  /// **'No terminal'**
  String get terminalNoTerminal;

  /// No description provided for @terminalClear.
  ///
  /// In en, this message translates to:
  /// **'Clear the scrollback'**
  String get terminalClear;

  /// No description provided for @terminalDropped.
  ///
  /// In en, this message translates to:
  /// **'The oldest output was dropped.'**
  String get terminalDropped;

  /// No description provided for @terminalReady.
  ///
  /// In en, this message translates to:
  /// **'The shell is ready. Type a command.'**
  String get terminalReady;

  /// No description provided for @terminalHint.
  ///
  /// In en, this message translates to:
  /// **'a command, then enter'**
  String get terminalHint;

  /// No description provided for @terminalSend.
  ///
  /// In en, this message translates to:
  /// **'Send'**
  String get terminalSend;

  /// No description provided for @terminalClosed.
  ///
  /// In en, this message translates to:
  /// **'closed'**
  String get terminalClosed;

  /// No description provided for @chatEmptyMessage.
  ///
  /// In en, this message translates to:
  /// **'Send a prompt to start the conversation.'**
  String get chatEmptyMessage;

  /// No description provided for @chatEmptyTitle.
  ///
  /// In en, this message translates to:
  /// **'No messages yet'**
  String get chatEmptyTitle;
}

class _AppLocalizationsDelegate
    extends LocalizationsDelegate<AppLocalizations> {
  const _AppLocalizationsDelegate();

  @override
  Future<AppLocalizations> load(Locale locale) {
    return SynchronousFuture<AppLocalizations>(lookupAppLocalizations(locale));
  }

  @override
  bool isSupported(Locale locale) =>
      <String>['en', 'it'].contains(locale.languageCode);

  @override
  bool shouldReload(_AppLocalizationsDelegate old) => false;
}

AppLocalizations lookupAppLocalizations(Locale locale) {
  // Lookup logic when only language code is specified.
  switch (locale.languageCode) {
    case 'en':
      return AppLocalizationsEn();
    case 'it':
      return AppLocalizationsIt();
  }

  throw FlutterError(
    'AppLocalizations.delegate failed to load unsupported locale "$locale". This is likely '
    'an issue with the localizations generation tool. Please file an issue '
    'on GitHub with a reproducible sample app and the gen-l10n configuration '
    'that was used.',
  );
}
