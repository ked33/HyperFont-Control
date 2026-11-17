package local.hyperfont;

import android.graphics.Typeface;
import android.os.Build;
import android.os.SharedMemory;
import java.io.FileOutputStream;
import java.nio.ByteBuffer;
import java.util.Map;

/** Called once before font mounts are exposed. No font files are copied. */
public final class StockFontMap {
    public static void main(String[] args) {
        try {
            if (args.length != 1 || Build.VERSION.SDK_INT < 31 || Build.VERSION.SDK_INT > 35)
                throw new IllegalArgumentException("supported: Android 12 through 15");
            Class<?> system = Class.forName("android.graphics.fonts.SystemFonts");
            Class<?> configClass = Class.forName("android.text.FontConfig");
            Object config = system.getMethod("getSystemPreinstalledFontConfig").invoke(null);
            Object fallback = system.getMethod("buildSystemFallback", configClass).invoke(null, config);
            Object map = system.getMethod("buildSystemTypefaces", configClass, Map.class)
                    .invoke(null, config, fallback);
            if (!((Map<?, ?>) map).containsKey("sans-serif"))
                throw new IllegalStateException("empty original font map");
            SharedMemory memory = (SharedMemory) Typeface.class.getMethod("serializeFontMap", Map.class)
                    .invoke(null, map);
            ByteBuffer buffer = memory.mapReadOnly();
            try (FileOutputStream output = new FileOutputStream(args[0])) {
                byte[] block = new byte[65536];
                while (buffer.hasRemaining()) {
                    int count = Math.min(block.length, buffer.remaining());
                    buffer.get(block, 0, count);
                    output.write(block, 0, count);
                }
            } finally { SharedMemory.unmap(buffer); memory.close(); }
            System.out.println("HFC_STOCK_FONT_MAP_READY");
        } catch (Throwable error) {
            System.err.println("HFC_STOCK_FONT_MAP_FAILED " + error);
            System.exit(1);
        }
    }
}
