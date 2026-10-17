# CI matrix

P = passed, F = failed, F* = failed then passed on retry. A test absent from a run passed.

R1=https://github.com/dovholuknf/atrium/actions/runs/38992005406, R2=https://github.com/dovholuknf/atrium/actions/runs/38996454669, R3=https://github.com/dovholuknf/atrium/actions/runs/38996589488, R4=https://github.com/dovholuknf/atrium/actions/runs/38998557699, R5=https://github.com/dovholuknf/atrium/actions/runs/39000519389, R6=https://github.com/dovholuknf/atrium/actions/runs/39000703278, R7=https://github.com/dovholuknf/atrium/actions/runs/39003297482, R8=https://github.com/dovholuknf/atrium/actions/runs/39032382613 (8c58ebd5), R9=https://github.com/dovholuknf/atrium/actions/runs/39034662485 (b7f498eb)

```
testname                                                   os       area          R1 R2 R3 R4 R5 R6 R7 R8 R9
changeReq                                                  linux    board-visual  P P P P P P P F P
changeReq                                                  macos    board-visual  P P P P P P P P F
changeReq                                                  windows  board-visual  P P P P P P F F F
chatTab                                                    linux    board-visual  P P P P P P P P F
chatTab                                                    windows  board-visual  P P P P P P F P F
chatter                                                    windows  board         F P P P P P P P P
clearKeepsPage                                             linux    board         F P P P P P P P P
clearKeepsPage                                             windows  board         F P P P P P P P P
detailsOne                                                 linux    board         F P P P P P P P P
detailsOne                                                 macos    board         F P P P P P P P P
detailsOne                                                 windows  board         F P P P P P P P P
groupBridge                                                linux    board         P F P P P P P P P
groupBridge                                                macos    board         P P P F P P P F P
groupBridge                                                windows  board         P P P P P P F F F
groupColor                                                 linux    board         F P P P P P P P P
groupColor                                                 macos    board         F P P P P P P P P
groupColor                                                 windows  board         F P P P P P P P P
hubReposFeed                                               windows  board         P P P F P P P P P
idleRate                                                   linux    board         P P P P P P F P P
mBubbles                                                   windows  board         P F P P P P P P P
mCardUpload                                                linux    board         F P P P P P P P P
mChatThumbs                                                linux    board         P P F P P P P P P
mChatThumbs                                                macos    board         F P F P P P P P P
mWorking                                                   windows  board-visual  P P P P P P F P F
newWorkDialog                                              windows  board         P P P F P P P P P
pasteBig                                                   macos    board         P P P P P F P P P
pasteLands                                                 macos    board-visual  P P P P P P F F P
pasteSpinner                                               macos    board         P P P P P P F P F
peekOneHover                                               macos    board         P P P F P P P P P
phoneFollow                                                macos    board-visual  P P P P P P F P P
phoneKeyLabel                                              windows  board-visual  P P P P P P P F P
phoneKeyLit                                                macos    board-visual  P P P P P P F P F
phonePan                                                   macos    board-visual  P P P P P P P P F
pinNow                                                     windows  board         P F P P P P P P P
pullsDrawer                                                linux    board         F P P P P P P P P
pullsDrawer                                                macos    board         F P P P P P P P P
pullsDrawer                                                windows  board         F P P P P P P P P
readTabSize                                                macos    board         P P P P P P F P P
resumeSpinner                                              linux    board-visual  P P P P F* F* P P P
switchPrewarm                                              macos    board-visual  P P P P P P F P P
switchPrewarm                                              windows  board-visual  P P P P P P F P F
termDebug                                                  linux    board         F P P P P P P P P
termDebug                                                  macos    board         F P P P P P P P P
termDebug                                                  windows  board         F P P P P P P P P
termMinionHidden                                           linux    board         F P P P P P P P P
termProbeStorm                                             macos    board         F P P P P P P P P
TestALaunchWithNoBriefCarriesTheLineOnItsPromptOnce        windows  go            P P F P P P P P P
TestAnOutsideCodeCardsBriefAndPromptCarryNoGitURLLine      windows  go            P P F P P P P P P
TestARepositoryMadeByAPushThatTakesNothingIsTakenAway      windows  go            P P P P P P P P F
TestASayToADoneCardIsKeptThenTypedInWhenWakeResumesIt      windows  go            P P P P P P P F P
TestConcurrentAttachesAllEstablish                         linux    go            P P F P P P P P P
TestConcurrentAttachesAllEstablish                         macos    go            P F P P P P P P P
TestConcurrentAttachesAllEstablish                         windows  go            P P F P P P P P P
TestLoadedProcessDriftsMore                                macos    go            P P P F P P P P P
TestOpenAnIssueClaimsItsKeyAndRefusesOneAnotherRoomHolds   windows  go            P P P P P P P F P
TestOpenMakesTheWorktreeTheRowAndTheCardAndTiesThem        macos    go            F P P P P P P P P
TestStartRunsADetachedHostFromACopy                        windows  go            P P F P P F F P P
TestTheGateCountsEveryStreamOnASingleRoomHub               linux    go            P P P P P P F P P
TestTheGateCountsEveryStreamOnATwoRoomHub                  linux    go            P P P P P P P P F
TestUnparkByPeerDoesNotStamp                               windows  go            P P P P F P P P P
themePreview                                               linux    board         F P P P P P P P P
themePreview                                               macos    board         F P P P P P P P P
themePreview                                               windows  board         F P P P P P P P P
u016                                                       linux    board         F P P P P P P P P
u016                                                       macos    board         F P P P P P P P P
u016                                                       windows  board         F P P P P P P P P
usagePolish                                                linux    board         P P F* P P P P P P
usageTab                                                   macos    board         P F P P P P P P P
walkContext                                                linux    board         F P P P P P P P P
walkContext                                                macos    board         F P P P P P P P P
walkContext                                                windows  board         F P P P P P P P P
websiteSkin                                                macos    board-visual  P P P P P P F P P
```
