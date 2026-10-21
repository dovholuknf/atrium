Two flaky internal/api tests fixed: the task event comparison ignores the clock-read idle_seconds, and a measure racing a close no longer overwrites the size the close recorded (apievent-flakes).
