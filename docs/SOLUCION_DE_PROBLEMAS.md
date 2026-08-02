# Solución de problemas

## La app no abre o aparece una ventana negra

pasaDATOS no necesita una consola. El ejecutable oficial está compilado como aplicación GUI.

Revisá:

1. que estés usando `pasaDATOS-Setup-Windows-x64.exe` o `pasaDATOS-Windows-x64.exe` del directorio `release/`;
2. que Microsoft Edge esté instalado;
3. el archivo `%APPDATA%\pasaDATOS\pasaDATOS.log`;
4. si el puerto `8765` ya está en uso.

Comprobar el puerto:

```powershell
Get-NetTCPConnection -LocalPort 8765 -ErrorAction SilentlyContinue
```

## El celular no abre la dirección de la PC

- Verificá que ambos estén en el mismo Wi‑Fi.
- Configurá la red de Windows como **Privada**.
- Desactivá temporalmente VPN en ambos equipos.
- Algunas redes empresariales o de invitados activan aislamiento entre clientes.
- Probá la otra dirección IP mostrada por `ipconfig` si la PC tiene adaptadores virtuales.
- Reinstalá para recrear la regla de firewall.

## El QR abre una página que no carga

El QR local contiene una IP privada. Solo funciona conectado a la misma red. Para usarlo desde otra red, seleccioná **Remoto** y generá el código con el relay conectado.

## El modo remoto dice “Sin conexión”

Comprobá:

```bash
curl -v https://pasadatos.tudominio.com/api/v1/health
```

Revisá:

- DNS;
- certificado TLS;
- contenedor;
- red Traefik;
- etiquetas del router;
- `PASADATOS_PUBLIC_URL`;
- logs: `docker logs pasadatos-relay`.

## Error 413 o “archivo demasiado grande”

El error suele venir del proxy. En Nginx:

```nginx
client_max_body_size 0;
proxy_request_buffering off;
```

También revisá `PASADATOS_MAX_FILE_BYTES` y el espacio del volumen.

## La transferencia se corta con archivos grandes

- Evitá suspender la PC o bloquear el celular durante una carga móvil.
- Aumentá timeouts del proxy.
- Verificá ahorro de batería.
- No cambies de red durante la transferencia.
- Usá Wi‑Fi local para archivos muy grandes cuando sea posible.

## El archivo no aparece en la PC

- Verificá **Ajustes → Descargar automáticamente**.
- Abrí la carpeta configurada desde pasaDATOS.
- Revisá el historial por un error.
- Confirmá espacio libre y permisos de escritura.
- Revisá `%APPDATA%\pasaDATOS\pasaDATOS.log`.

## El celular no muestra la ruta final

Es una restricción de Android/iOS y del navegador. El sistema administra la ubicación final. pasaDATOS registra “Descargas del dispositivo”.

## SmartScreen muestra “Editor desconocido”

Los ejecutables no incluyen firma Authenticode comercial. Comprobá el hash SHA‑256 publicado en `release/SHA256SUMS.txt` antes de ejecutar.

## Restablecer toda la configuración Windows

1. Cerrá completamente pasaDATOS.
2. Renombrá `%APPDATA%\pasaDATOS` a `pasaDATOS-backup`.
3. Abrí la app.

Se generará una identidad nueva. Deberás volver a vincular los dispositivos.
