# Seguridad y límites

## Modelo de confianza

pasaDATOS está pensado para uso personal, familiar o de equipos pequeños con un relay administrado por el propio usuario.

No es un servicio multiempresa con cuotas, facturación, auditoría inmutable, antivirus centralizado o moderación de contenido.

## Identidad de dispositivo

Cada instalación genera:

- un ID aleatorio;
- un token secreto aleatorio de 256 bits;
- un nombre visible.

El servidor guarda un hash SHA‑256 del token. Las solicitudes autenticadas envían el ID y el token. Un dispositivo no puede acceder a transferencias ajenas.

## Vinculación

- El código tiene 10 caracteres y se presenta como `ABCDE-FGHIJ`.
- Es de un solo uso.
- Vence automáticamente.
- No permite vincular un dispositivo consigo mismo.
- Una transferencia solo puede crearse entre dispositivos vinculados.

## Descargas

- La descarga autenticada requiere credenciales del receptor.
- La app móvil puede solicitar un enlace temporal de descarga.
- El token de descarga tiene vencimiento corto.
- El servidor admite un único rango HTTP para reanudar descargas.

## Interfaz nativa de Windows

Los endpoints `/native/*`:

- solamente aceptan conexiones desde loopback;
- exigen un token privado generado en la PC;
- no se exponen al celular ni al relay remoto;
- permiten abrir selectores, rutas y configurar la app.

## Transporte

### Local

El tráfico dentro de la red local usa HTTP. Esto evita certificados locales y simplifica el acceso por IP. Usalo únicamente en redes de confianza.

La regla de firewall creada por el instalador se limita al perfil **Privado** de Windows.

### Remoto

Debe usarse HTTPS en el proxy inverso. El binario del relay escucha HTTP internamente y delega TLS en Traefik, Nginx o Caddy.

pasaDATOS no implementa cifrado de extremo a extremo. El administrador del VPS tiene acceso técnico a los archivos temporales almacenados en disco.

## Almacenamiento temporal

- Los archivos remotos se guardan en `data/files/`.
- Se eliminan al vencer el TTL o después de la entrega y el período de gracia.
- Los metadatos finalizados se eliminan según su TTL.
- La eliminación normal del sistema de archivos no equivale a borrado forense seguro.

## Nombres de archivo

El servidor:

- elimina componentes de ruta;
- reemplaza caracteres inseguros;
- limita la longitud;
- evita traversal fuera del directorio de almacenamiento;
- nunca sobrescribe un archivo recibido existente en Windows.

## Tipos de archivo

pasaDATOS no bloquea extensiones. Eso significa que también puede transferir ejecutables, scripts o archivos potencialmente maliciosos.

Buenas prácticas:

- transferí solo archivos esperados;
- no ejecutes archivos de origen desconocido;
- mantené activo Microsoft Defender o el antivirus del equipo;
- escaneá contenido recibido cuando corresponda.

## Tamaño máximo

`PASADATOS_MAX_FILE_BYTES=0` elimina el límite de aplicación. No elimina límites físicos o externos:

- disco;
- cuota del VPS;
- memoria y almacenamiento temporal del navegador;
- proxy inverso;
- sistema de archivos;
- tiempo de conexión;
- políticas de la red móvil.

## Firma de código de Windows

Los ejecutables incluidos no están firmados con un certificado comercial de Authenticode. Windows SmartScreen puede mostrar una advertencia de editor desconocido.

La firma requiere un certificado emitido al propietario del software y no puede incorporarse de manera legítima sin ese certificado privado.

Para distribución pública se recomienda:

1. obtener un certificado de firma de código;
2. firmar el instalador y el ejecutable;
3. aplicar sello de tiempo;
4. publicar hashes SHA‑256;
5. distribuir desde un dominio o repositorio oficial.

## Riesgos operativos

- Un enlace temporal copiado a otra persona puede usarse hasta que venza.
- Un token de dispositivo robado permite actuar como ese dispositivo.
- Borrar datos del navegador elimina la identidad móvil local, pero no revoca automáticamente el vínculo en otros equipos.
- Un relay sin HTTPS expone credenciales y archivos al tránsito de red.
- Una copia de seguridad de `state.json` contiene hashes y metadatos sensibles.
