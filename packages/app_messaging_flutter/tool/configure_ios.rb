gem 'xcodeproj', '>= 1.27.0'
require 'xcodeproj'

root = ARGV.fetch(0)
ios = File.join(root, 'ios')
project_path = File.join(ios, 'Runner.xcodeproj')
project = Xcodeproj::Project.open(project_path)
target = project.targets.find { |t| t.name == 'Runner' }
abort 'Expected an application target named Runner' unless target
google_path = File.join(ios, 'Runner', 'GoogleService-Info.plist')
google = Xcodeproj::Plist.read_from_path(google_path)
info_path = File.join(ios, 'Runner', 'Info.plist')
info = Xcodeproj::Plist.read_from_path(info_path)
target.build_configurations.each do |config|
  id = config.build_settings['PRODUCT_BUNDLE_IDENTIFIER']
  abort 'GoogleService-Info.plist does not match PRODUCT_BUNDLE_IDENTIFIER' unless id == google['BUNDLE_ID']
end
abort 'FirebaseAppDelegateProxyEnabled must not be disabled' if info['FirebaseAppDelegateProxyEnabled'] == false
exit 0 if ARGV.include?('--check')

runner = project.main_group.find_subpath('Runner', false)
abort 'Expected a Runner file group' unless runner
google_ref = runner.files.find { |f| f.path == 'GoogleService-Info.plist' } || runner.new_file('GoogleService-Info.plist')
target.resources_build_phase.add_file_reference(google_ref, true)

target.build_configurations.each do |config|
  # Preserve custom entitlements and unrelated capabilities.
  relative = config.build_settings['CODE_SIGN_ENTITLEMENTS'] || 'Runner/Runner.entitlements'
  abort 'Resolve custom entitlement variables before configure' if relative.include?('$(')
  path = File.join(ios, relative)
  entitlement = File.exist?(path) ? Xcodeproj::Plist.read_from_path(path) : {}
  entitlement['aps-environment'] ||= '$(APP_MESSAGING_APNS_ENVIRONMENT)'
  Xcodeproj::Plist.write_to_path(entitlement, path)
  config.build_settings['CODE_SIGN_ENTITLEMENTS'] = relative
  # Override this build setting for custom development-signed Release builds.
  environment = entitlement['aps-environment']
  config.build_settings['APP_MESSAGING_APNS_ENVIRONMENT'] ||= (
    %w[development production].include?(environment) ? environment : (config.name.start_with?('Debug') ? 'development' : 'production')
  )
end

attributes = project.root_object.attributes['TargetAttributes'] ||= {}
capabilities = (attributes[target.uuid] ||= {})['SystemCapabilities'] ||= {}
capabilities['com.apple.Push'] = { 'enabled' => 1 }
capabilities['com.apple.BackgroundModes'] = { 'enabled' => 1 }
info['UIBackgroundModes'] = ((info['UIBackgroundModes'] || []) + ['remote-notification']).uniq
info['AppMessagingAPNSEnvironment'] = '$(APP_MESSAGING_APNS_ENVIRONMENT)'
Xcodeproj::Plist.write_to_path(info, info_path)
project.save
puts 'iOS: Google Services, Push Notifications и background remote notifications настроены.'
