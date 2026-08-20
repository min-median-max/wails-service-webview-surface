module github.com/soksak/wails-service-webview-surface

go 1.25.0

require (
	github.com/soksak/soksak-contract-contentview v0.0.1
	github.com/soksak/wails-service-native-compositor v0.0.1
)

replace github.com/soksak/soksak-contract-contentview => ../../soksak-contracts/soksak-contract-contentview

replace github.com/soksak/wails-service-native-compositor => ../wails-service-native-compositor
