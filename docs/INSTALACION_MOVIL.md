# App móvil — Android y iPhone

pasaDATOS móvil es una **aplicación web instalable** incluida dentro del servidor. No requiere una cuenta, una tienda de aplicaciones ni una compilación diferente para Android y iPhone.

## Android

1. Abrí la URL de pasaDATOS en Chrome.
2. Tocá el aviso **Instalá pasaDATOS**, o abrí el menú de Chrome.
3. Elegí **Instalar aplicación** o **Agregar a pantalla principal**.
4. Confirmá.

La app aparecerá con su icono. Cuando el acceso se abre desde el relay remoto por HTTPS, Chrome puede instalarla como PWA completa. Desde una IP local por HTTP, algunos Android muestran únicamente **Agregar a pantalla principal**; el acceso sigue funcionando para transferir mientras la PC esté encendida y en la misma red.

## iPhone y iPad

1. Abrí la URL en Safari.
2. Tocá el botón **Compartir**.
3. Elegí **Agregar a inicio**.
4. Activá **Abrir como app web** cuando Safari muestre esa opción.
5. Confirmá el nombre `pasaDATOS`.

## Uso local y remoto

El origen desde el que instalás la app determina a qué servidor se conecta:

- si la abrís desde `http://192.168.x.x:8765`, se conecta a la PC por Wi‑Fi local;
- si la abrís desde `https://pasadatos.tudominio.com`, se conecta al relay remoto.

Los navegadores no permiten que una app HTTPS acceda libremente a un servidor HTTP de la red local debido a las reglas de contenido mixto. Por eso, para alternar entre ambos modos podés:

- mantener instalada la versión remota y abrir el enlace local desde el navegador cuando estés en casa u oficina; o
- agregar ambos accesos a la pantalla de inicio y distinguirlos por el contexto de uso.

## Descargas y rutas

Android e iOS deciden dónde guardar cada archivo. Según el navegador, tipo de archivo y configuración, puede ocurrir que:

- se guarde en Descargas;
- se abra una vista previa;
- aparezca el panel Compartir;
- se pregunte en qué aplicación guardarlo.

Por diseño de seguridad, pasaDATOS no puede leer la ruta física final del archivo en el celular.

## Archivos grandes

La app envía el archivo directamente como un flujo HTTP. No lo convierte ni lo comprime. Durante cargas grandes:

- mantené la app visible;
- evitá que el teléfono entre en ahorro extremo de energía;
- no cambies de Wi‑Fi a datos móviles a mitad de la carga;
- verificá el espacio libre en destino.

## Restablecer la identidad móvil

La identidad se guarda en el almacenamiento local del sitio. Para crear una identidad nueva:

1. quitá el vínculo desde los otros dispositivos;
2. borrá los datos del sitio en el navegador;
3. volvé a abrir pasaDATOS y vinculá nuevamente.
