package local.hyperfont;

import android.graphics.Paint;
import android.graphics.Typeface;
import android.graphics.fonts.Font;
import android.graphics.text.PositionedGlyphs;
import android.graphics.text.TextRunShaper;
import android.os.Parcel;
import android.os.SharedMemory;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.util.Map;
import org.json.JSONArray;
import org.json.JSONObject;

/** Diagnostic only; excluded from installable JARs. Run in a private namespace. */
public final class ExemptionProbe {
    private static native boolean install(String stock, int incomingSize);
    private static native boolean consumed();
    private static native boolean restore();
    private static native int step();
    private static native String fdName();

    public static void main(String[] args) throws Exception {
        System.load(args[0]);
        Class<?> system = Class.forName("android.graphics.fonts.SystemFonts");
        Class<?> configClass = Class.forName("android.text.FontConfig");
        Object config = system.getMethod("getSystemPreinstalledFontConfig").invoke(null);
        Object fallback = system.getMethod("buildSystemFallback", configClass).invoke(null, config);
        Object map = system.getMethod("buildSystemTypefaces", configClass, Map.class).invoke(null, config, fallback);
        SharedMemory incoming = (SharedMemory) Typeface.class.getMethod("serializeFontMap", Map.class).invoke(null, map);
        Parcel parcel = Parcel.obtain();incoming.writeToParcel(parcel, 0);parcel.setDataPosition(0);
        boolean files = restore();
        if (!files) throw new IllegalStateException("files restore failed at step " + step());
        if (!install(args[1], incoming.getSize())) throw new IllegalStateException("hook unavailable");
        // This matches incoming BIND_APPLICATION SharedMemory deserialization.
        SharedMemory stock = SharedMemory.CREATOR.createFromParcel(parcel);
        Typeface.class.getMethod("setSystemFontMap", SharedMemory.class).invoke(null, stock);
        if (!consumed()) throw new IllegalStateException("fontMap FD name did not match: " + fdName());
        Paint paint = new Paint();paint.setTextSize(32);paint.setTypeface(Typeface.create("sans-serif", 0));
        String text = "美食外卖超市便利韩国炸鸡啤酒屋Abc123";
        PositionedGlyphs glyphs = TextRunShaper.shapeTextRun(text, 0, text.length(), 0, text.length(), 0, 0, false, paint);
        JSONArray rows = new JSONArray();
        for (int i=0;i<glyphs.glyphCount();i++) {
            Font font=glyphs.getFont(i);
            rows.put(new JSONObject().put("file",font.getFile()==null?JSONObject.NULL:font.getFile().getPath())
                    .put("glyph",glyphs.getGlyphId(i)));
        }
        JSONObject result=new JSONObject().put("descriptorSubstituted",consumed()).put("filesRestored",files)
                .put("incomingBytes",incoming.getSize()).put("stockBytes",stock.getSize()).put("glyphs",rows);
        ByteBuffer buffer=stock.mapReadOnly().order(ByteOrder.BIG_ENDIAN);
        result.put("stockHeaderBytes",buffer.getInt());SharedMemory.unmap(buffer);
        System.out.println(result.toString());
        parcel.recycle();stock.close();incoming.close();
    }
}
