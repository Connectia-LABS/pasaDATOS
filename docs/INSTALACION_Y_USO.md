# Instalación y manual de uso — pasaDATOS

## Requisitos

### PC

- Windows 10 u 11 de 64 bits.
- Microsoft Edge instalado. La aplicación usa Edge en modo aplicación para mostrar su interfaz, sin barras ni pestañas.
- Permiso para aceptar la solicitud de Control de cuentas de usuario durante la instalación.
- Para el modo Wi‑Fi, la red de Windows debe estar configurada como **Privada**.

### Celular

- Android con Chrome u otro navegador moderno, o iPhone/iPad con Safari.
- Para el modo local, conexión a la misma red Wi‑Fi que la PC.
- Para el modo remoto, acceso a Internet y una URL HTTPS del relay.

## Instalación recomendada en Windows

1. Ejecutá `pasaDATOS-Setup-Windows-x64.exe`.
2. Aceptá la autorización de Windows.
3. Al finalizar, se abrirá pasaDATOS.

La instalación se realiza en:

```text
%LOCALAPPDATA%\pasaDATOS
```

Los datos privados y el historial se guardan en:

```text
%APPDATA%\pasaDATOS
```

La carpeta predeterminada para archivos recibidos es:

```text
%USERPROFILE%\Downloads\pasaDATOS
```

El instalador agrega una regla de firewall limitada al ejecutable de pasaDATOS, al puerto `8765/TCP` y al perfil de red **Privado**.

## Versión portátil

`pasaDATOS-Windows-x64.exe` puede ejecutarse sin instalar. La primera ejecución crea la configuración en `%APPDATA%\pasaDATOS`.

En la versión portátil, Windows puede pedir permiso de firewall la primera vez. Permitilo únicamente en redes privadas.

## Vinculación por Wi‑Fi local

1. Abrí pasaDATOS en la PC.
2. Verificá que el selector superior diga **Wi‑Fi local**.
3. Entrá en **Dispositivos**.
4. Tocá **Generar código**.
5. En el celular, abrí la URL LAN indicada o escaneá el QR.
6. La app móvil se registra automáticamente y utiliza el código del enlace.
7. El celular aparecerá en **Dispositivos vinculados**.

El vínculo persiste aunque el código venza. El código solamente sirve para crear el vínculo inicial.

## Enviar desde Windows

1. Entrá en **Enviar**.
2. Arrastrá archivos al área central o tocá **Elegir archivos**.
3. Seleccioná el dispositivo de destino.
4. Tocá **Enviar archivos**.
5. Seguí el progreso en **Transferencias activas**.

En modo local, el archivo permanece en su ruta original y se transmite directamente cuando el celular lo descarga. No se crea una copia adicional en la PC.

En modo remoto, el archivo se transmite al relay en streaming y queda disponible temporalmente para el receptor.

## Recibir en Windows

Cuando **Descargar automáticamente** está activado:

1. el remitente carga el archivo;
2. la PC detecta el archivo listo;
3. pasaDATOS lo descarga en segundo plano;
4. lo guarda en la carpeta configurada;
5. marca la transferencia como recibida;
6. registra la ruta completa en el historial.

Si ya existe un archivo con el mismo nombre, se crea automáticamente:

```text
archivo (1).ext
archivo (2).ext
```

Nunca se sobrescribe un archivo existente.

## Enviar desde el celular

1. En la app móvil, abrí **Enviar**.
2. Tocá **Buscar en el celular**.
3. Seleccioná uno o varios archivos.
4. Elegí el dispositivo.
5. Tocá **Enviar archivos**.

La transferencia muestra progreso individual. El navegador mantiene la app activa durante la carga; para archivos muy grandes conviene evitar bloquear el teléfono o cambiar de red.

## Recibir en el celular

1. Abrí **Recibir**.
2. Tocá **Descargar** en el archivo pendiente.
3. El sistema operativo administra la descarga o abre el panel de compartir/guardar, según el formato y la plataforma.

Por restricciones de privacidad del sistema móvil, una aplicación web no puede conocer la ruta física final elegida por Android o iOS. El historial registra el destino como **Descargas del dispositivo**.

## Historial

### Windows

Registra:

- nombre y tamaño;
- fecha y hora;
- enviado o recibido;
- modo local o remoto;
- dispositivo contraparte;
- estado;
- ruta de origen;
- ruta de destino.

El botón de abrir ubicación muestra el archivo o su carpeta en el Explorador.

### Móvil

El historial se guarda en el almacenamiento local del navegador. Limpiar datos del sitio, cambiar de navegador o desinstalar la app puede borrarlo.

## Cambiar carpeta de recepción

1. Entrá en **Ajustes**.
2. En **Este equipo**, tocá **Elegir**.
3. Seleccioná la carpeta.
4. Guardá los cambios.

## Desinstalación

Usá cualquiera de estas opciones:

- **Configuración de Windows → Aplicaciones → Aplicaciones instaladas → pasaDATOS → Desinstalar**.
- Acceso `Desinstalar pasaDATOS.exe` dentro de la carpeta de instalación.

La desinstalación quita el programa, accesos directos, regla de firewall, inicio automático e historial. Los archivos ya recibidos en Descargas no se eliminan.
