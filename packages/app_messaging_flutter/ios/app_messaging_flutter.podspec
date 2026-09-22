Pod::Spec.new do |s|
  s.name = 'app_messaging_flutter'
  s.version = '0.0.0'
  s.summary = 'Firebase and AppMetrica push integration.'
  s.homepage = 'https://github.com/NikitaVasin/pocket_mfo'
  s.author = 'NikitaVasin'
  s.source = { :path => '.' }
  s.source_files = 'app_messaging_flutter/Sources/app_messaging_flutter/**/*.{h,m}'
  s.public_header_files = 'app_messaging_flutter/Sources/app_messaging_flutter/include/*.h'
  s.dependency 'Flutter'
  s.dependency 'AppMetricaPush', '~> 3.4.0'
  s.platform = :ios, '15.0'
end
