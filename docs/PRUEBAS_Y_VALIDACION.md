# Pruebas y validación

## Pruebas automatizadas

Ejecutar:

```bash
go test -race ./...
go vet ./...
node --check internal/pasadatos/web/assets/desktop.js
node --check internal/pasadatos/web/assets/mobile.js
```

La suite cubre:

- registro de dos dispositivos;
- creación y consumo de código de vinculación;
- rechazo de dispositivos no vinculados;
- rechazo de credenciales incorrectas;
- creación de transferencia;
- carga de contenido binario, incluidos bytes nulos y no ASCII;
- progreso de carga;
- listado de bandeja del receptor;
- descarga parcial mediante `Range`;
- descarga completa a disco;
- confirmación de entrega;
- persistencia de destino;
- saneamiento de nombres;
- prevención de colisiones de nombre.

## Validación de compilación

Los artefactos se comprueban con `file`:

```text
pasaDATOS-Windows-x64.exe: PE32+ executable for MS Windows, x86-64, GUI
pasaDATOS-Setup-Windows-x64.exe: PE32+ executable for MS Windows, x86-64, GUI
pasadatos-server-linux-amd64: ELF 64-bit, x86-64, statically linked
```

## Validación ejecutada sobre los binarios de entrega

Además de la suite automatizada, se ejecutaron pruebas contra el binario Linux final del mismo motor que usa Windows:

- registro y vinculación real de PC y móvil simulado;
- transferencia móvil → PC con guardado automático y comparación SHA-256;
- transferencia PC → móvil con comparación byte a byte;
- descarga parcial HTTP `Range` y reanudación;
- actualización del historial y destino final;
- carga de la interfaz móvil, manifiesto, iconos, CSS, JavaScript y service worker;
- verificación de que los ejecutables Windows son PE32+ GUI x64 y el relay es ELF x64 estático.

La instalación UAC, accesos directos, regla de Firewall y comportamiento de Safari/Chrome siguen requiriendo la comprobación final en hardware real, porque el entorno de compilación utilizado es Linux.

## Prueba manual local recomendada

1. Instalar en Windows.
2. Abrir la URL LAN en Android y en iPhone.
3. Vincular cada dispositivo.
4. Enviar:
   - archivo vacío;
   - imagen JPEG;
   - video grande;
   - ZIP;
   - archivo con tildes y espacios;
   - varios archivos juntos.
5. Repetir un nombre para verificar `archivo (1).ext`.
6. Interrumpir una descarga remota y reintentar.
7. Verificar historial y rutas.
8. Cambiar la red de Windows a Pública y confirmar que el firewall no expone el puerto según la regla instalada.

## Prueba manual remota recomendada

1. Desplegar el relay con HTTPS.
2. Vincular PC y celular usando redes distintas.
3. Enviar en ambos sentidos.
4. Reiniciar el contenedor durante una transferencia y verificar el estado de error/reintento.
5. Confirmar limpieza del archivo temporal después de la entrega.
6. Verificar espacio del volumen `data/`.

## Aspectos que requieren prueba en hardware real

La compilación cruzada valida formato y arquitectura, pero no reemplaza una prueba final en:

- Windows 10;
- Windows 11;
- Android/Chrome;
- iPhone/Safari;
- red Wi‑Fi con aislamiento de clientes;
- firewall o antivirus corporativo.
