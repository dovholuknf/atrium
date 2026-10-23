The older-claude autocompact test no longer flakes on Linux: its fake claude is exec'd once until no writer holds it, so the probe never sees ETXTBSY (q-autocompact-older-claude).
