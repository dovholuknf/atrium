Docs: none, a layout fix with nothing a reader can name

## Test plan

## @LETTER@. Long text in the notification tray wraps

### @LETTER@1. A long path and a run URL

1. Raise a notice whose body holds a long Windows path, for example a handoff file under
   `C:\Users\<you>\AppData\Local\Temp\atrium\handoffs\`, and a CI notice with its GitHub run URL.
2. Raise enough other notices that the tray list scrolls, then open the tray from the bell.

**Expected:** the path and the URL wrap inside their rows. There is no horizontal scrollbar, and there is one vertical
scrollbar, the list's. Headless unit `notifyTrayWrap`.
