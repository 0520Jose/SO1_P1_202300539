import time
a = []
while True:
	a.append(' ' * 1024 * 1024)
	if len(a) > 500:
		time.sleep(0.1)
