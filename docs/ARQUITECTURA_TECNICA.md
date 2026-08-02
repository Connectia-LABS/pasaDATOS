# Arquitectura técnica

## Componentes

### 1. Aplicación Windows

Un ejecutable Go autónomo que:

- inicia un servidor HTTP local en `0.0.0.0:8765`;
- mantiene la identidad del equipo;
- sirve la interfaz móvil y la interfaz de escritorio;
- abre Microsoft Edge en modo aplicación;
- muestra selectores nativos mediante PowerShell/Windows Forms;
- envía archivos locales sin copiarlos previamente;
- descarga archivos entrantes a la carpeta configurada;
- persiste historial y configuración en JSON atómico;
- se conecta al relay remoto como cliente cuando corresponde.

### 2. Aplicación móvil

PWA responsive servida por el hub local o el relay remoto:

- identidad anónima en `localStorage`;
- registro automático;
- selección múltiple de archivos;
- carga raw por `XMLHttpRequest` con progreso;
- descarga mediante token temporal;
- vinculación por código/QR;
- historial local;
- manifest, iconos y service worker.

### 3. Relay remoto

El mismo binario Go ejecutado con `--server`:

- API HTTP;
- almacenamiento temporal en disco;
- autenticación de dispositivos;
- enlaces de dispositivo;
- códigos temporales;
- transferencias y estados;
- descargas con Range;
- limpieza automática.

### 4. Instalador Windows

Ejecutable autónomo que contiene:

- `pasaDATOS.exe`;
- icono;
- guía rápida.

Configura accesos directos, firewall privado, inicio automático y desinstalación.

## Flujo local PC → móvil

```text
PC selecciona archivo
      │
      ▼
Hub local crea transferencia "linked"
      │
      ▼
Móvil consulta bandeja y pide token
      │
      ▼
Hub abre el archivo original y lo transmite
      │
      ▼
Móvil inicia descarga y marca recibido
      │
      ▼
PC actualiza historial
```

El archivo original no se duplica en la carpeta de spool.

## Flujo local móvil → PC

```text
Móvil crea transferencia
      │
      ▼
PUT en streaming al spool local
      │
      ▼
Callback al DesktopAgent
      │
      ▼
Copia atómica a Descargas\pasaDATOS
      │
      ▼
Marca recibido y programa limpieza del spool
```

## Flujo remoto

```text
Remitente → HTTPS proxy → relay → disco temporal
                                      │
Receptor consulta bandeja ────────────┘
      │
      ▼
Descarga autenticada/reanudable
      │
      ▼
Marca entrega → limpieza programada
```

## API principal

| Método | Ruta | Descripción |
|---|---|---|
| `GET` | `/api/v1/health` | Salud y versión |
| `PUT` | `/api/v1/devices/{id}` | Registrar/actualizar dispositivo |
| `GET` | `/api/v1/me/peers` | Listar vinculados |
| `DELETE` | `/api/v1/me/peers/{peerID}` | Desvincular |
| `POST` | `/api/v1/pairings` | Crear código |
| `POST` | `/api/v1/pairings/join` | Usar código |
| `POST` | `/api/v1/transfers` | Crear transferencia |
| `PUT` | `/api/v1/transfers/{id}/content` | Cargar contenido |
| `GET` | `/api/v1/transfers` | Bandeja/historial del servidor |
| `GET` | `/api/v1/transfers/{id}/content` | Descargar autenticado |
| `POST` | `/api/v1/transfers/{id}/download-token` | Crear enlace temporal |
| `POST` | `/api/v1/transfers/{id}/received` | Confirmar entrega |
| `DELETE` | `/api/v1/transfers/{id}` | Cancelar |

## Persistencia

No requiere base de datos externa. Usa JSON atómico protegido por mutex:

- `state.json`: relay/hub;
- `config.json`: identidad y ajustes Windows;
- `history.json`: historial Windows.

Las escrituras se realizan sobre un archivo temporal y luego se renombran.

## Concurrencia

- Mutex de lectura/escritura para estado persistente.
- Jobs de transferencia independientes.
- Progreso persistido con throttling para evitar escrituras excesivas.
- Mapa de transferencias entrantes en curso para impedir descargas duplicadas.
- Cliente HTTP sin timeout global durante streams largos.

## Compatibilidad

- Ejecutable Windows: `GOOS=windows`, `GOARCH=amd64`, `CGO_ENABLED=0`.
- Relay: Linux amd64 estático.
- Interfaz: navegadores modernos con Fetch, Web Crypto, localStorage y File API.
