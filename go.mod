module github.com/soksak/wails-service-webview-surface

go 1.25.0

require (
	github.com/soksak/soksak-core v0.0.0
	github.com/soksak/wails-service-native-compositor v0.0.1
)

replace github.com/soksak/wails-service-native-compositor => ../wails-service-native-compositor

replace github.com/soksak/soksak-core => ../../soksak-core
