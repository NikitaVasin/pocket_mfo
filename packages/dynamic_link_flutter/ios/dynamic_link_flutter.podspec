Pod::Spec.new do |s|
  s.name = 'dynamic_link_flutter'
  s.version = '0.1.0'
  s.summary = 'Dynamic links and WebView data cleanup.'
  s.description = s.summary
  s.homepage = 'https://example.com/mfo-hub'
  s.license = { :type => 'Proprietary' }
  s.author = { 'MFO HUB' => 'dev@example.com' }
  s.source = { :path => '.' }
  s.source_files = 'dynamic_link_flutter/Sources/dynamic_link_flutter/**/*'
  s.dependency 'Flutter'
  s.frameworks = 'WebKit'
  s.platform = :ios, '15.0'
  s.swift_version = '5.0'
end
