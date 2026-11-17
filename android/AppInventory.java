package local.hyperfont;

import android.content.pm.ApplicationInfo;
import android.content.pm.PackageManager;
import org.json.JSONArray;
import org.json.JSONObject;

/** On-demand only; never launched in the boot path. */
public final class AppInventory {
    public static void main(String[] args) {
        try {
            android.os.Looper.prepareMainLooper();
            Class<?> thread = Class.forName("android.app.ActivityThread");
            Object instance = thread.getMethod("systemMain").invoke(null);
            android.content.Context context = (android.content.Context)
                    thread.getMethod("getSystemContext").invoke(instance);
            PackageManager manager = context.getPackageManager();
            JSONArray result = new JSONArray();
            for (ApplicationInfo app : manager.getInstalledApplications(0)) {
                if (app.uid % 100000 < 10000 || app.uid % 100000 >= 90000) continue;
                result.put(new JSONObject().put("package", app.packageName)
                        .put("label", app.loadLabel(manager).toString()).put("uid", app.uid)
                        .put("system", (app.flags & ApplicationInfo.FLAG_SYSTEM) != 0));
            }
            System.out.println(result.toString());
        } catch (Throwable error) {
            System.err.println("HFC_APP_INVENTORY_FAILED " + error);
            error.printStackTrace();
            System.exit(1);
        }
    }
}
