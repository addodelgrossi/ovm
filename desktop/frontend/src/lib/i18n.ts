import type { ApiError, Locale } from './api'

export const en = {
  'app.documentTitle': 'ovm — ONVIF-compatible device manager',
  'app.name': 'ovm',
  'app.product': 'Device manager',
  'app.version': 'Version {version}',
  'app.loading': 'Opening ovm…',
  'app.loadingDetail': 'Loading saved devices and network interfaces.',
  'app.errorTitle': 'ovm could not start',
  'app.errorDetail':
    'The desktop service did not provide the application state.',
  'action.retry': 'Try again',
  'action.refresh': 'Refresh',
  'action.close': 'Close',
  'action.cancel': 'Cancel',
  'action.save': 'Save',
  'action.dismiss': 'Dismiss',
  'action.authenticate': 'Authenticate',
  'action.forget': 'Forget',
  'action.copy': 'Copy',
  'action.copied': 'Copied',
  'action.openVlc': 'Open in VLC',
  'preferences.language': 'Language',
  'preferences.theme': 'Theme',
  'preferences.theme.system': 'System',
  'preferences.theme.light': 'Light',
  'preferences.theme.dark': 'Dark',
  'sidebar.label': 'Devices and discovery',
  'sidebar.search': 'Search devices',
  'sidebar.searchPlaceholder': 'Name, endpoint, or status',
  'sidebar.interface': 'Mac network interface',
  'sidebar.interfaceAutomatic': 'Automatic',
  'sidebar.scan': 'Scan network',
  'sidebar.scanning': 'Scanning…',
  'sidebar.cancelScan': 'Cancel scan',
  'sidebar.cancelling': 'Cancelling…',
  'sidebar.manualAdd': 'Add manually',
  'sidebar.noDevices': 'No devices yet',
  'sidebar.noDevicesHint':
    'Scan the local network or add a device endpoint manually.',
  'sidebar.noResults': 'No devices match “{query}”.',
  'sidebar.selected': 'Selected',
  'sidebar.deviceActions': 'Actions for {name}',
  'sidebar.interfaceAddress': '{name} — {addresses}',
  'sidebar.interfaceReason': '{name} — unavailable: {reason}',
  'diagnostic.title': 'Discovery help',
  'diagnostic.noInterface': 'No usable multicast interface was reported.',
  'diagnostic.honest':
    'Local Network permission, interface state, VPNs, or network isolation can prevent discovery. ovm cannot determine which condition applies.',
  'diagnostic.backend': 'Discovery reported: {detail}',
  'diagnostic.manual':
    'Manual add remains available and does not depend on multicast discovery.',
  'demo.badge': 'Demo',
  'demo.claim':
    'Synthetic demo metadata only — no physical camera or playable RTSP stream is claimed.',
  'main.noSelectionEyebrow': 'Getting started',
  'main.noSelectionTitle': 'Connect your first camera',
  'main.noSelectionBody':
    'Discover ONVIF-compatible devices on the local network or enter a device service endpoint directly.',
  'main.stepOne': 'Choose the Mac interface connected to the camera network.',
  'main.stepTwo': 'Scan the network, or use manual add at any time.',
  'main.stepThree':
    'Authenticate to inspect device details and media profiles.',
  'main.localTitle': 'Local by design',
  'main.localBody': 'No telemetry and no cloud dependency.',
  'main.secretTitle': 'Credentials protected',
  'main.secretBody':
    'Passwords are handed to the native secret boundary and are not retained by this UI.',
  'main.safeTitle': 'Safe presentation',
  'main.safeBody':
    'Presented media URLs omit credentials and query parameters.',
  'header.unknownDevice': 'Selected device',
  'header.endpoint': 'Device service endpoint',
  'header.refreshShortcut': 'Refresh (⌘/Ctrl+R)',
  'tabs.label': 'Device views',
  'tabs.overview': 'Overview',
  'tabs.streams': 'Streams',
  'tabs.overviewShortcut': 'Overview (⌘/Ctrl+1)',
  'tabs.streamsShortcut': 'Streams (⌘/Ctrl+2)',
  'device.endpoint': 'Endpoint',
  'device.source': 'Discovery source',
  'device.source.manual': 'Manual',
  'device.source.ws_discovery': 'WS-Discovery',
  'device.source.unknown': 'Unknown',
  'device.credentials': 'Credentials',
  'device.credentialsStored': 'Stored in the OS secret service',
  'device.credentialsMissing': 'Not stored',
  'status.unknown': 'Unknown',
  'status.reachable': 'Reachable',
  'status.authentication_required': 'Authentication required',
  'status.unreachable': 'Unreachable',
  'status.error': 'Error',
  'overview.eyebrow': 'Device inventory',
  'overview.title': 'Overview',
  'overview.identity': 'Identity',
  'overview.detailsUnavailable':
    'Device details are not loaded for this session.',
  'overview.detailsStored':
    'Stored credentials exist, but ovm does not retain inventory between sessions. Authenticate again to load current details.',
  'overview.detailsMissing':
    'Authenticate to request current details from the device.',
  'overview.manufacturer': 'Manufacturer',
  'overview.model': 'Model',
  'overview.firmware': 'Firmware',
  'overview.serial': 'Serial number',
  'overview.hardware': 'Hardware ID',
  'overview.hostname': 'Hostname',
  'overview.hostnameDhcp': 'Provided by DHCP',
  'overview.hostnameStatic': 'Configured on camera',
  'overview.capabilities': 'Capabilities',
  'overview.services': 'Services',
  'overview.scopes': 'Scopes',
  'overview.noCapabilities': 'No capability endpoints were reported.',
  'overview.noServices': 'No services were reported.',
  'overview.noScopes': 'No scopes were reported.',
  'overview.hostInterface': 'Mac interface',
  'overview.hostInterfaceHint': 'Used by this Mac for discovery traffic.',
  'overview.cameraInterfaces': 'Camera interfaces',
  'overview.cameraInterfacesHint':
    'Reported by the camera; these are not interfaces on this Mac.',
  'overview.noCameraInterfaces': 'No camera network interfaces were reported.',
  'overview.addresses': 'Addresses',
  'overview.macAddress': 'MAC address',
  'overview.mtu': 'MTU',
  'overview.enabled': 'Enabled',
  'overview.disabled': 'Disabled',
  'overview.dhcp': 'DHCP',
  'overview.manual': 'Manual addressing',
  'overview.operations': 'Operation support',
  'overview.operation': 'Operation',
  'overview.support': 'Support',
  'overview.detail': 'Detail',
  'overview.clock': 'Camera clock',
  'overview.cameraTime': 'Camera UTC',
  'overview.clockDifference': 'Difference from this Mac',
  'overview.clockUncertainty': 'Estimated uncertainty',
  'overview.clockValue': '{difference} s (± {uncertainty} s)',
  'overview.clockWarning':
    'The camera clock differs by more than one second. The estimate includes request latency and the camera timestamp’s one-second precision.',
  'overview.clockOkay':
    'The estimated camera clock difference is within one second.',
  'overview.clockUnavailable':
    'The camera did not report a complete UTC timestamp.',
  'capability.device': 'Device',
  'capability.media': 'Media',
  'capability.events': 'Events',
  'capability.imaging': 'Imaging',
  'capability.ptz': 'PTZ',
  'capability.analytics': 'Analytics',
  'support.supported': 'Supported',
  'support.unsupported': 'Unsupported',
  'support.unknown': 'Unknown',
  'support.temporarily_unavailable': 'Temporarily unavailable',
  'operation.system_date_and_time': 'System date and time',
  'operation.device_information': 'Device information',
  'operation.capabilities': 'Capabilities',
  'operation.services': 'Services',
  'operation.scopes': 'Scopes',
  'operation.hostname': 'Hostname',
  'operation.network_interfaces': 'Network interfaces',
  'operation.profiles': 'Profiles',
  'operation.video_source': 'Video source',
  'operation.video_encoder': 'Video encoder',
  'operation.audio_encoder': 'Audio encoder',
  'operation.stream_tcp': 'TCP stream URI',
  'operation.stream_udp': 'UDP stream URI',
  'operation.snapshot_uri': 'Snapshot URI',
  'streams.eyebrow': 'Media profiles',
  'streams.title': 'Streams',
  'streams.loading': 'Loading media profiles…',
  'streams.empty': 'The device did not report any media profiles.',
  'streams.errorTitle': 'Some stream information is unavailable',
  'streams.mediaServices': 'Media services',
  'streams.mediaAvailable': 'available',
  'streams.mediaUnavailable': 'unavailable',
  'streams.profile': 'Profile {name}',
  'streams.fixed': 'Fixed profile',
  'streams.dynamic': 'Configurable profile',
  'streams.provenance': 'Profile source: {source}',
  'streams.source.media2': 'Media2',
  'streams.source.media1': 'Media1',
  'streams.source.unknown': 'Unknown service',
  'streams.fallback': 'Media1 fallback',
  'streams.video': 'Video',
  'streams.audio': 'Audio',
  'streams.codec': 'Codec',
  'streams.resolution': 'Resolution',
  'streams.frameRate': 'Frame rate',
  'streams.frameRateValue': '{value} fps',
  'streams.bitrate': 'Bitrate',
  'streams.bitrateValue': '{value} kbps',
  'streams.gop': 'GOP length',
  'streams.encoderProfile': 'Encoder profile',
  'streams.sampleRate': 'Sample rate',
  'streams.sampleRateValue': '{value} kHz',
  'streams.noVideo': 'No video encoder details were reported.',
  'streams.noAudio': 'No audio encoder details were reported.',
  'streams.urls': 'Safe presentation URLs',
  'streams.safeUrlNotice':
    'URLs shown by ovm intentionally omit credentials. They may require authentication in the receiving application.',
  'streams.tcp': 'RTSP over TCP',
  'streams.udp': 'RTSP over UDP',
  'streams.snapshot': 'Snapshot',
  'streams.urlUnavailable': 'Unavailable',
  'streams.copyLabel': 'Copy {kind} URL for {profile}',
  'streams.copiedLabel': '{kind} URL copied for {profile}',
  'streams.copyFailed':
    'Could not copy the URL. Clipboard access is unavailable.',
  'streams.vlcUnavailable':
    'VLC was not found at a supported local installation path.',
  'streams.vlcDemo':
    'VLC is disabled in demo mode because no real RTSP stream exists. Live preview shows a synthetic test pattern.',
  'live.start': 'Start live preview',
  'live.stop': 'Stop preview',
  'live.label': 'Live preview of {profile}',
  'live.connecting': 'Connecting to the preview…',
  'live.playing': 'Live',
  'live.ended': 'The preview ended.',
  'live.failed': 'The live preview could not start.',
  'live.mseUnavailable':
    'This WebView does not support Media Source Extensions, so live video cannot play here.',
  'live.codecUnsupported': 'This WebView cannot decode the {codec} stream.',
  'live.statsLabel': 'Measured stream statistics',
  'live.statResolution': 'Resolution',
  'live.statFps': 'Frame rate',
  'live.statBitrate': 'Bitrate',
  'live.statGop': 'GOP',
  'live.statFirstFrame': 'First frame',
  'live.statLost': 'Lost packets',
  'live.gopValue': '{frames} frames ({seconds} s)',
  'streams.playbackUnavailable': 'RTSP playback is unavailable in this build.',
  'streams.operations': 'Profile operations',
  'streams.operationSource': '{operation}: {state} via {source}',
  'streams.operationFallback':
    '{operation}: {state} via {source} (Media1 fallback)',
  'dialog.manualTitle': 'Add a device manually',
  'dialog.manualBody':
    'Enter a camera host, host and port, or HTTP(S) device service URL. Query parameters are not accepted.',
  'dialog.endpoint': 'Device endpoint',
  'dialog.endpointPlaceholder':
    '192.0.2.10 or https://camera.example/onvif/device_service',
  'dialog.displayName': 'Display name (optional)',
  'dialog.addDevice': 'Add device',
  'dialog.authTitle': 'Authenticate to {name}',
  'dialog.authBody':
    'Credentials are used to prove access before they are saved by the native OS secret service.',
  'dialog.username': 'Username',
  'dialog.password': 'Password',
  'dialog.passwordHint':
    'The password clears when you submit or close this dialog.',
  'dialog.policyTitle': 'Compatibility policies',
  'dialog.basic': 'Allow HTTP Basic authentication',
  'dialog.basicRisk':
    'Basic uses reusable credentials. ovm restricts it to HTTPS unless you separately allow plaintext HTTP.',
  'dialog.basicHttp': 'Allow Basic over plaintext HTTP',
  'dialog.basicHttpRisk':
    'High risk: network observers may read reusable credentials. Enable only on an isolated, trusted network.',
  'dialog.legacyDigest': 'Allow legacy Digest without qop',
  'dialog.legacyDigestRisk':
    'Compatibility risk: this weaker legacy challenge mode provides fewer replay protections.',
  'dialog.authSubmit': 'Authenticate',
  'dialog.forgetTitle': 'Forget {name}?',
  'dialog.forgetBody':
    'This removes device metadata and its native secret reference from ovm. It does not change the camera.',
  'dialog.forgetConfirm': 'Forget device',
  'toast.deviceAdded': 'Device added and selected.',
  'toast.scanComplete': 'Scan complete: {added} added, {updated} updated.',
  'toast.scanCancelled': 'Scan cancellation requested.',
  'toast.authenticated':
    'Authentication succeeded and device details were loaded.',
  'toast.forgotten': 'Device forgotten.',
  'toast.preferenceSaved': 'Preference saved.',
  'toast.refreshed': 'Application state refreshed.',
  'toast.vlcOpened': 'VLC launch requested.',
  'reason.usable': 'usable',
  'reason.down': 'interface is down',
  'reason.loopback_excluded': 'loopback excluded',
  'reason.point_to_point': 'point-to-point interface',
  'reason.multicast_unsupported': 'multicast unsupported',
  'reason.no_usable_ipv4': 'no usable IPv4 address',
  'reason.address_unavailable': 'address unavailable',
  'errors.bridge':
    'The desktop service is unavailable. Run ovm inside the Wails application.',
  'errors.invalidResponse': 'The desktop service returned an invalid response.',
  'errors.cancelled': 'The operation was cancelled.',
  'errors.timeout':
    'The device did not respond before the operation timed out.',
  'errors.authentication':
    'Authentication failed. Check the credentials and compatibility policies.',
  'errors.network':
    'The network operation failed. Check the selected interface and device address.',
  'errors.vlcUnavailable':
    'VLC is unavailable at the supported installation paths.',
  'errors.invalidRequest':
    'The request was rejected before any device operation.',
  'errors.secret': 'The native secret service could not complete the request.',
  'errors.generic': '{message} (code: {code})'
} as const

export type TranslationKey = keyof typeof en
export type TranslationValues = Record<string, string | number>

export const ptBR: Record<TranslationKey, string> = {
  'app.documentTitle':
    'ovm — gerenciador de dispositivos compatíveis com ONVIF',
  'app.name': 'ovm',
  'app.product': 'Gerenciador de dispositivos',
  'app.version': 'Versão {version}',
  'app.loading': 'Abrindo o ovm…',
  'app.loadingDetail': 'Carregando dispositivos salvos e interfaces de rede.',
  'app.errorTitle': 'Não foi possível iniciar o ovm',
  'app.errorDetail': 'O serviço do aplicativo não forneceu o estado inicial.',
  'action.retry': 'Tentar novamente',
  'action.refresh': 'Atualizar',
  'action.close': 'Fechar',
  'action.cancel': 'Cancelar',
  'action.save': 'Salvar',
  'action.dismiss': 'Dispensar',
  'action.authenticate': 'Autenticar',
  'action.forget': 'Esquecer',
  'action.copy': 'Copiar',
  'action.copied': 'Copiado',
  'action.openVlc': 'Abrir no VLC',
  'preferences.language': 'Idioma',
  'preferences.theme': 'Tema',
  'preferences.theme.system': 'Sistema',
  'preferences.theme.light': 'Claro',
  'preferences.theme.dark': 'Escuro',
  'sidebar.label': 'Dispositivos e descoberta',
  'sidebar.search': 'Buscar dispositivos',
  'sidebar.searchPlaceholder': 'Nome, endpoint ou status',
  'sidebar.interface': 'Interface de rede do Mac',
  'sidebar.interfaceAutomatic': 'Automática',
  'sidebar.scan': 'Buscar na rede',
  'sidebar.scanning': 'Buscando…',
  'sidebar.cancelScan': 'Cancelar busca',
  'sidebar.cancelling': 'Cancelando…',
  'sidebar.manualAdd': 'Adicionar manualmente',
  'sidebar.noDevices': 'Nenhum dispositivo',
  'sidebar.noDevicesHint':
    'Busque na rede local ou adicione manualmente o endpoint de um dispositivo.',
  'sidebar.noResults': 'Nenhum dispositivo corresponde a “{query}”.',
  'sidebar.selected': 'Selecionado',
  'sidebar.deviceActions': 'Ações para {name}',
  'sidebar.interfaceAddress': '{name} — {addresses}',
  'sidebar.interfaceReason': '{name} — indisponível: {reason}',
  'diagnostic.title': 'Ajuda para descoberta',
  'diagnostic.noInterface':
    'Nenhuma interface multicast utilizável foi informada.',
  'diagnostic.honest':
    'A permissão de Rede Local, o estado da interface, VPNs ou o isolamento da rede podem impedir a descoberta. O ovm não consegue determinar qual condição se aplica.',
  'diagnostic.backend': 'A descoberta informou: {detail}',
  'diagnostic.manual':
    'A adição manual continua disponível e não depende da descoberta multicast.',
  'demo.badge': 'Demonstração',
  'demo.claim':
    'Somente metadados sintéticos — nenhuma câmera física ou transmissão RTSP reproduzível é alegada.',
  'main.noSelectionEyebrow': 'Primeiros passos',
  'main.noSelectionTitle': 'Conecte sua primeira câmera',
  'main.noSelectionBody':
    'Descubra dispositivos compatíveis com ONVIF na rede local ou informe diretamente um endpoint do serviço do dispositivo.',
  'main.stepOne': 'Escolha a interface do Mac conectada à rede da câmera.',
  'main.stepTwo': 'Busque na rede ou use a adição manual a qualquer momento.',
  'main.stepThree':
    'Autentique para consultar detalhes do dispositivo e perfis de mídia.',
  'main.localTitle': 'Local por princípio',
  'main.localBody': 'Sem telemetria e sem dependência de nuvem.',
  'main.secretTitle': 'Credenciais protegidas',
  'main.secretBody':
    'As senhas são entregues ao armazenamento nativo e não ficam retidas nesta interface.',
  'main.safeTitle': 'Apresentação segura',
  'main.safeBody':
    'As URLs de mídia exibidas omitem credenciais e parâmetros de consulta.',
  'header.unknownDevice': 'Dispositivo selecionado',
  'header.endpoint': 'Endpoint do serviço do dispositivo',
  'header.refreshShortcut': 'Atualizar (⌘/Ctrl+R)',
  'tabs.label': 'Visualizações do dispositivo',
  'tabs.overview': 'Visão geral',
  'tabs.streams': 'Transmissões',
  'tabs.overviewShortcut': 'Visão geral (⌘/Ctrl+1)',
  'tabs.streamsShortcut': 'Transmissões (⌘/Ctrl+2)',
  'device.endpoint': 'Endpoint',
  'device.source': 'Origem da descoberta',
  'device.source.manual': 'Manual',
  'device.source.ws_discovery': 'WS-Discovery',
  'device.source.unknown': 'Desconhecida',
  'device.credentials': 'Credenciais',
  'device.credentialsStored': 'Armazenadas no serviço secreto do sistema',
  'device.credentialsMissing': 'Não armazenadas',
  'status.unknown': 'Desconhecido',
  'status.reachable': 'Acessível',
  'status.authentication_required': 'Autenticação necessária',
  'status.unreachable': 'Inacessível',
  'status.error': 'Erro',
  'overview.eyebrow': 'Inventário do dispositivo',
  'overview.title': 'Visão geral',
  'overview.identity': 'Identidade',
  'overview.detailsUnavailable':
    'Os detalhes do dispositivo não foram carregados nesta sessão.',
  'overview.detailsStored':
    'Há credenciais armazenadas, mas o ovm não mantém o inventário entre sessões. Autentique novamente para carregar detalhes atuais.',
  'overview.detailsMissing':
    'Autentique para solicitar detalhes atuais ao dispositivo.',
  'overview.manufacturer': 'Fabricante',
  'overview.model': 'Modelo',
  'overview.firmware': 'Firmware',
  'overview.serial': 'Número de série',
  'overview.hardware': 'ID de hardware',
  'overview.hostname': 'Nome do host',
  'overview.hostnameDhcp': 'Fornecido por DHCP',
  'overview.hostnameStatic': 'Configurado na câmera',
  'overview.capabilities': 'Capacidades',
  'overview.services': 'Serviços',
  'overview.scopes': 'Escopos',
  'overview.noCapabilities': 'Nenhum endpoint de capacidade foi informado.',
  'overview.noServices': 'Nenhum serviço foi informado.',
  'overview.noScopes': 'Nenhum escopo foi informado.',
  'overview.hostInterface': 'Interface do Mac',
  'overview.hostInterfaceHint':
    'Usada por este Mac para o tráfego de descoberta.',
  'overview.cameraInterfaces': 'Interfaces da câmera',
  'overview.cameraInterfacesHint':
    'Informadas pela câmera; não são interfaces deste Mac.',
  'overview.noCameraInterfaces':
    'Nenhuma interface de rede da câmera foi informada.',
  'overview.addresses': 'Endereços',
  'overview.macAddress': 'Endereço MAC',
  'overview.mtu': 'MTU',
  'overview.enabled': 'Ativada',
  'overview.disabled': 'Desativada',
  'overview.dhcp': 'DHCP',
  'overview.manual': 'Endereçamento manual',
  'overview.operations': 'Suporte a operações',
  'overview.operation': 'Operação',
  'overview.support': 'Suporte',
  'overview.detail': 'Detalhe',
  'overview.clock': 'Relógio da câmera',
  'overview.cameraTime': 'UTC da câmera',
  'overview.clockDifference': 'Diferença em relação a este Mac',
  'overview.clockUncertainty': 'Incerteza estimada',
  'overview.clockValue': '{difference} s (± {uncertainty} s)',
  'overview.clockWarning':
    'O relógio da câmera difere em mais de um segundo. A estimativa inclui a latência da solicitação e a precisão de um segundo do timestamp da câmera.',
  'overview.clockOkay':
    'A diferença estimada do relógio da câmera está dentro de um segundo.',
  'overview.clockUnavailable':
    'A câmera não informou um timestamp UTC completo.',
  'capability.device': 'Dispositivo',
  'capability.media': 'Mídia',
  'capability.events': 'Eventos',
  'capability.imaging': 'Imagem',
  'capability.ptz': 'PTZ',
  'capability.analytics': 'Análise',
  'support.supported': 'Suportada',
  'support.unsupported': 'Não suportada',
  'support.unknown': 'Desconhecida',
  'support.temporarily_unavailable': 'Temporariamente indisponível',
  'operation.system_date_and_time': 'Data e hora do sistema',
  'operation.device_information': 'Informações do dispositivo',
  'operation.capabilities': 'Capacidades',
  'operation.services': 'Serviços',
  'operation.scopes': 'Escopos',
  'operation.hostname': 'Nome do host',
  'operation.network_interfaces': 'Interfaces de rede',
  'operation.profiles': 'Perfis',
  'operation.video_source': 'Fonte de vídeo',
  'operation.video_encoder': 'Codificador de vídeo',
  'operation.audio_encoder': 'Codificador de áudio',
  'operation.stream_tcp': 'URI de transmissão TCP',
  'operation.stream_udp': 'URI de transmissão UDP',
  'operation.snapshot_uri': 'URI de captura',
  'streams.eyebrow': 'Perfis de mídia',
  'streams.title': 'Transmissões',
  'streams.loading': 'Carregando perfis de mídia…',
  'streams.empty': 'O dispositivo não informou perfis de mídia.',
  'streams.errorTitle':
    'Algumas informações de transmissão estão indisponíveis',
  'streams.mediaServices': 'Serviços de mídia',
  'streams.mediaAvailable': 'disponível',
  'streams.mediaUnavailable': 'indisponível',
  'streams.profile': 'Perfil {name}',
  'streams.fixed': 'Perfil fixo',
  'streams.dynamic': 'Perfil configurável',
  'streams.provenance': 'Origem do perfil: {source}',
  'streams.source.media2': 'Media2',
  'streams.source.media1': 'Media1',
  'streams.source.unknown': 'Serviço desconhecido',
  'streams.fallback': 'Fallback para Media1',
  'streams.video': 'Vídeo',
  'streams.audio': 'Áudio',
  'streams.codec': 'Codec',
  'streams.resolution': 'Resolução',
  'streams.frameRate': 'Taxa de quadros',
  'streams.frameRateValue': '{value} fps',
  'streams.bitrate': 'Taxa de bits',
  'streams.bitrateValue': '{value} kbps',
  'streams.gop': 'Tamanho do GOP',
  'streams.encoderProfile': 'Perfil do codificador',
  'streams.sampleRate': 'Taxa de amostragem',
  'streams.sampleRateValue': '{value} kHz',
  'streams.noVideo': 'Nenhum detalhe do codificador de vídeo foi informado.',
  'streams.noAudio': 'Nenhum detalhe do codificador de áudio foi informado.',
  'streams.urls': 'URLs seguras para apresentação',
  'streams.safeUrlNotice':
    'As URLs exibidas pelo ovm omitem intencionalmente as credenciais. Elas podem exigir autenticação no aplicativo receptor.',
  'streams.tcp': 'RTSP sobre TCP',
  'streams.udp': 'RTSP sobre UDP',
  'streams.snapshot': 'Captura',
  'streams.urlUnavailable': 'Indisponível',
  'streams.copyLabel': 'Copiar URL {kind} de {profile}',
  'streams.copiedLabel': 'URL {kind} copiada de {profile}',
  'streams.copyFailed':
    'Não foi possível copiar a URL. O acesso à área de transferência está indisponível.',
  'streams.vlcUnavailable':
    'O VLC não foi encontrado em um caminho de instalação local compatível.',
  'streams.vlcDemo':
    'O VLC é desativado na demonstração porque não existe uma transmissão RTSP real. A prévia ao vivo mostra um padrão de teste sintético.',
  'live.start': 'Iniciar prévia ao vivo',
  'live.stop': 'Parar prévia',
  'live.label': 'Prévia ao vivo de {profile}',
  'live.connecting': 'Conectando à prévia…',
  'live.playing': 'Ao vivo',
  'live.ended': 'A prévia terminou.',
  'live.failed': 'Não foi possível iniciar a prévia ao vivo.',
  'live.mseUnavailable':
    'Este WebView não oferece Media Source Extensions, então o vídeo ao vivo não pode ser reproduzido aqui.',
  'live.codecUnsupported':
    'Este WebView não consegue decodificar a transmissão {codec}.',
  'live.statsLabel': 'Estatísticas medidas da transmissão',
  'live.statResolution': 'Resolução',
  'live.statFps': 'Taxa de quadros',
  'live.statBitrate': 'Taxa de bits',
  'live.statGop': 'GOP',
  'live.statFirstFrame': 'Primeiro quadro',
  'live.statLost': 'Pacotes perdidos',
  'live.gopValue': '{frames} quadros ({seconds} s)',
  'streams.playbackUnavailable':
    'A reprodução RTSP está indisponível nesta compilação.',
  'streams.operations': 'Operações do perfil',
  'streams.operationSource': '{operation}: {state} via {source}',
  'streams.operationFallback':
    '{operation}: {state} via {source} (fallback para Media1)',
  'dialog.manualTitle': 'Adicionar um dispositivo manualmente',
  'dialog.manualBody':
    'Informe um host, host e porta, ou URL HTTP(S) do serviço do dispositivo. Parâmetros de consulta não são aceitos.',
  'dialog.endpoint': 'Endpoint do dispositivo',
  'dialog.endpointPlaceholder':
    '192.0.2.10 ou https://camera.example/onvif/device_service',
  'dialog.displayName': 'Nome de exibição (opcional)',
  'dialog.addDevice': 'Adicionar dispositivo',
  'dialog.authTitle': 'Autenticar em {name}',
  'dialog.authBody':
    'As credenciais são usadas para comprovar o acesso antes de serem salvas pelo armazenamento secreto nativo.',
  'dialog.username': 'Usuário',
  'dialog.password': 'Senha',
  'dialog.passwordHint': 'A senha é apagada ao enviar ou fechar este diálogo.',
  'dialog.policyTitle': 'Políticas de compatibilidade',
  'dialog.basic': 'Permitir autenticação HTTP Basic',
  'dialog.basicRisk':
    'Basic usa credenciais reutilizáveis. O ovm restringe seu uso a HTTPS, a menos que você permita HTTP em texto simples separadamente.',
  'dialog.basicHttp': 'Permitir Basic sobre HTTP em texto simples',
  'dialog.basicHttpRisk':
    'Alto risco: observadores da rede podem ler credenciais reutilizáveis. Ative somente em uma rede isolada e confiável.',
  'dialog.legacyDigest': 'Permitir Digest legado sem qop',
  'dialog.legacyDigestRisk':
    'Risco de compatibilidade: este modo de desafio legado mais fraco oferece menos proteção contra repetição.',
  'dialog.authSubmit': 'Autenticar',
  'dialog.forgetTitle': 'Esquecer {name}?',
  'dialog.forgetBody':
    'Isso remove os metadados do dispositivo e sua referência secreta nativa do ovm. A câmera não é alterada.',
  'dialog.forgetConfirm': 'Esquecer dispositivo',
  'toast.deviceAdded': 'Dispositivo adicionado e selecionado.',
  'toast.scanComplete':
    'Busca concluída: {added} adicionado(s), {updated} atualizado(s).',
  'toast.scanCancelled': 'Cancelamento da busca solicitado.',
  'toast.authenticated':
    'Autenticação concluída e detalhes do dispositivo carregados.',
  'toast.forgotten': 'Dispositivo esquecido.',
  'toast.preferenceSaved': 'Preferência salva.',
  'toast.refreshed': 'Estado do aplicativo atualizado.',
  'toast.vlcOpened': 'Abertura do VLC solicitada.',
  'reason.usable': 'utilizável',
  'reason.down': 'interface inativa',
  'reason.loopback_excluded': 'loopback excluído',
  'reason.point_to_point': 'interface ponto a ponto',
  'reason.multicast_unsupported': 'multicast não suportado',
  'reason.no_usable_ipv4': 'sem endereço IPv4 utilizável',
  'reason.address_unavailable': 'endereço indisponível',
  'errors.bridge':
    'O serviço do aplicativo está indisponível. Execute o ovm dentro do aplicativo Wails.',
  'errors.invalidResponse':
    'O serviço do aplicativo retornou uma resposta inválida.',
  'errors.cancelled': 'A operação foi cancelada.',
  'errors.timeout': 'O dispositivo não respondeu antes do limite de tempo.',
  'errors.authentication':
    'A autenticação falhou. Verifique as credenciais e políticas de compatibilidade.',
  'errors.network':
    'A operação de rede falhou. Verifique a interface selecionada e o endereço do dispositivo.',
  'errors.vlcUnavailable':
    'O VLC está indisponível nos caminhos de instalação compatíveis.',
  'errors.invalidRequest':
    'A solicitação foi rejeitada antes de qualquer operação no dispositivo.',
  'errors.secret':
    'O serviço secreto nativo não conseguiu concluir a solicitação.',
  'errors.generic': '{message} (código: {code})'
}

const dictionaries: Record<Locale, Record<TranslationKey, string>> = {
  en,
  'pt-BR': ptBR
}

export function normalizeLocale(locale: string | undefined): Locale {
  return locale === 'pt-BR' ? 'pt-BR' : 'en'
}

export function t(
  locale: Locale,
  key: TranslationKey,
  values: TranslationValues = {}
): string {
  return dictionaries[locale][key].replace(
    /\{([A-Za-z0-9_]+)\}/g,
    (match, name) =>
      Object.hasOwn(values, name) ? String(values[name]) : match
  )
}

const statusKeys: Record<string, TranslationKey> = {
  unknown: 'status.unknown',
  reachable: 'status.reachable',
  authentication_required: 'status.authentication_required',
  unreachable: 'status.unreachable',
  error: 'status.error'
}

const supportKeys: Record<string, TranslationKey> = {
  supported: 'support.supported',
  unsupported: 'support.unsupported',
  unknown: 'support.unknown',
  temporarily_unavailable: 'support.temporarily_unavailable'
}

const operationKeys: Record<string, TranslationKey> = {
  system_date_and_time: 'operation.system_date_and_time',
  device_information: 'operation.device_information',
  capabilities: 'operation.capabilities',
  services: 'operation.services',
  scopes: 'operation.scopes',
  hostname: 'operation.hostname',
  network_interfaces: 'operation.network_interfaces',
  profiles: 'operation.profiles',
  video_source: 'operation.video_source',
  video_encoder: 'operation.video_encoder',
  audio_encoder: 'operation.audio_encoder',
  stream_tcp: 'operation.stream_tcp',
  stream_udp: 'operation.stream_udp',
  snapshot_uri: 'operation.snapshot_uri'
}

const reasonKeys: Record<string, TranslationKey> = {
  usable: 'reason.usable',
  down: 'reason.down',
  loopback_excluded: 'reason.loopback_excluded',
  point_to_point: 'reason.point_to_point',
  multicast_unsupported: 'reason.multicast_unsupported',
  no_usable_ipv4: 'reason.no_usable_ipv4',
  address_unavailable: 'reason.address_unavailable'
}

export function deviceStatusLabel(locale: Locale, status: string): string {
  return t(locale, statusKeys[status] ?? 'status.unknown')
}

export function supportLabel(locale: Locale, state: string): string {
  return t(locale, supportKeys[state] ?? 'support.unknown')
}

export function operationLabel(locale: Locale, operation: string): string {
  const key = operationKeys[operation]
  return key ? t(locale, key) : operation
}

export function interfaceReasonLabel(locale: Locale, reason: string): string {
  const key = reasonKeys[reason]
  return key ? t(locale, key) : reason
}

export function mediaSourceLabel(locale: Locale, source?: string): string {
  if (source === 'media2') return t(locale, 'streams.source.media2')
  if (source === 'media1') return t(locale, 'streams.source.media1')
  return t(locale, 'streams.source.unknown')
}

export function errorMessage(locale: Locale, error: ApiError): string {
  if (error.code === 'bridge_unavailable') return t(locale, 'errors.bridge')
  if (error.code === 'invalid_response')
    return t(locale, 'errors.invalidResponse')
  if (error.code === 'operation_cancelled' || error.category === 'cancelled') {
    return t(locale, 'errors.cancelled')
  }
  if (error.code === 'operation_timeout') return t(locale, 'errors.timeout')
  if (error.code === 'vlc_unavailable')
    return t(locale, 'errors.vlcUnavailable')
  if (error.category === 'authentication')
    return t(locale, 'errors.authentication')
  if (error.category === 'network') return t(locale, 'errors.network')
  if (error.category === 'usage') return t(locale, 'errors.invalidRequest')
  if (error.code.startsWith('secret_')) return t(locale, 'errors.secret')
  return t(locale, 'errors.generic', {
    message: error.message,
    code: error.code
  })
}
