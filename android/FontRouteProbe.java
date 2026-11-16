package local.hyperfont;

import android.graphics.Paint;
import android.graphics.Typeface;
import android.graphics.fonts.Font;
import android.graphics.text.PositionedGlyphs;
import android.graphics.text.TextRunShaper;
import android.os.Build;
import android.os.LocaleList;
import android.util.Base64;
import java.text.Bidi;
import org.json.JSONArray;
import org.json.JSONObject;

/** Read-only Android shaping evidence, separate from embedded app renderers. */
public final class FontRouteProbe {
    private static String initializeFontMap() throws Exception {
        if (Build.VERSION.SDK_INT < 31) return "process-default";
        // Standalone app_process does not receive ActivityThread's font map.
        // Prefer FontManager's live config (including downloaded system updates).
        try {
            Class<?> serviceManager = Class.forName("android.os.ServiceManager");
            Object binder = serviceManager.getMethod("getService", String.class).invoke(null, "font");
            Class<?> stub = Class.forName("com.android.internal.graphics.fonts.IFontManager$Stub");
            Object manager = stub.getMethod("asInterface", android.os.IBinder.class).invoke(null, binder);
            Object config = Class.forName("com.android.internal.graphics.fonts.IFontManager").getMethod("getFontConfig").invoke(manager);
            Class<?> systemFonts = Class.forName("android.graphics.fonts.SystemFonts");
            Class<?> configClass = Class.forName("android.text.FontConfig");
            Object fallback = systemFonts.getMethod("buildSystemFallback", configClass).invoke(null, config);
            Object map = systemFonts.getMethod("buildSystemTypefaces", configClass, java.util.Map.class).invoke(null, config, fallback);
            Typeface.class.getMethod("setSystemFontMap", java.util.Map.class).invoke(null, map);
            return "font-manager-config-rebuilt";
        } catch (ReflectiveOperationException | RuntimeException unavailable) {
            Typeface.class.getMethod("loadPreinstalledSystemFontMap").invoke(null);
            return "preinstalled-config-rebuilt";
        }
    }

    public static void main(String[] args) {
        try {
            if (args.length != 3) throw new IllegalArgumentException("text, locale and family required");
            String text = new String(Base64.decode(args[0], Base64.DEFAULT), "UTF-8");
            if (text.length() > 512) throw new IllegalArgumentException("sample too large");
            String fontMapSource = initializeFontMap();
            Paint paint = new Paint();
            paint.setTextSize(32);
            paint.setTypeface(Typeface.create(args[2], Typeface.NORMAL));
            if (!args[1].isEmpty()) paint.setTextLocales(LocaleList.forLanguageTags(args[1]));
            JSONArray rows = new JSONArray();
            JSONObject result = new JSONObject();
            result.put("family", args[2]);
            result.put("language", args[1]);
            result.put("sdk", Build.VERSION.SDK_INT);
            result.put("fontMapSource", fontMapSource);
            result.put("embeddedRendererVerified", false);
            if (Build.VERSION.SDK_INT < 31) {
                result.put("verification", "android-has-glyph-only");
                for (int i = 0; i < text.length();) {
                    int cp = text.codePointAt(i); String character = new String(Character.toChars(cp));
                    rows.put(new JSONObject().put("character", character).put("hasGlyph", paint.hasGlyph(character)));
                    i += Character.charCount(cp);
                }
            } else {
                result.put("verification", "android-actual-shaped-glyphs");
                Bidi bidi = new Bidi(text, Bidi.DIRECTION_DEFAULT_LEFT_TO_RIGHT);
                float advance = 0;
                for (int run = 0; run < bidi.getRunCount(); run++) {
                int start = bidi.getRunStart(run), end = bidi.getRunLimit(run);
                PositionedGlyphs glyphs = TextRunShaper.shapeTextRun(text, start, end-start, 0, text.length(), advance, 0, (bidi.getRunLevel(run)&1)!=0, paint);
                for (int i = 0; i < glyphs.glyphCount(); i++) {
                    Font font = glyphs.getFont(i);
                    JSONObject row = new JSONObject();
                    row.put("glyphId", glyphs.getGlyphId(i));
                    row.put("x", glyphs.getGlyphX(i)); row.put("y", glyphs.getGlyphY(i));
                    row.put("file", font.getFile() == null ? JSONObject.NULL : font.getFile().getPath());
                    row.put("ttcIndex", font.getTtcIndex());
                    row.put("weight", font.getStyle().getWeight());
                    row.put("runStart", start); row.put("runEnd", end);
                    rows.put(row);
                }
                advance += glyphs.getAdvance();
                }
            }
            result.put("glyphs", rows);
            System.out.println(result.toString());
        } catch (Throwable error) {
            System.err.println("HFC_GLYPH_PROBE_FAILED " + error.getClass().getSimpleName() + ": " + error.getMessage());
            System.exit(1);
        }
    }
}
