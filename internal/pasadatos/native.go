package pasadatos

func OpenDesktopWindow(url string) error { return nativeOpenApp(url) }

func SetNativeAutoStart(enabled bool) error { return nativeSetAutoStart(enabled) }
