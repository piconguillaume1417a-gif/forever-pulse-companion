// Cabinet extraction and in-memory MSI planning. No install transaction is executed.
using System;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;

internal static class InspectMsi
{
    [DllImport("msi.dll", CharSet = CharSet.Unicode)]
    private static extern uint MsiOpenDatabase(string path, IntPtr mode, out uint database);
    [DllImport("msi.dll", CharSet = CharSet.Unicode)]
    private static extern uint MsiDatabaseOpenView(uint database, string query, out uint view);
    [DllImport("msi.dll")] private static extern uint MsiViewExecute(uint view, uint record);
    [DllImport("msi.dll")] private static extern uint MsiViewFetch(uint view, out uint record);
    [DllImport("msi.dll")] private static extern uint MsiRecordReadStream(uint record, uint field, byte[] buffer, ref uint length);
    [DllImport("msi.dll")] private static extern uint MsiCloseHandle(uint handle);
    [DllImport("msi.dll", CharSet = CharSet.Unicode)]
    private static extern uint MsiOpenPackageEx(string package, uint options, out uint session);
    [DllImport("msi.dll", CharSet = CharSet.Unicode)]
    private static extern uint MsiDoAction(uint session, string action);
    [DllImport("msi.dll", CharSet = CharSet.Unicode)]
    private static extern uint MsiSetProperty(uint session, string property, string value);
    [DllImport("msi.dll", CharSet = CharSet.Unicode)]
    private static extern uint MsiGetProperty(uint session, string property, StringBuilder value, ref uint length);
    [DllImport("msi.dll", CharSet = CharSet.Unicode)]
    private static extern int MsiEvaluateCondition(uint session, string condition);
    [DllImport("msi.dll")] private static extern uint MsiSetInternalUI(uint level, IntPtr owner);
    private static void Check(uint code) { if (code != 0) throw new InvalidDataException("MSI read error: " + code); }
    private static string Property(uint session, string name)
    {
        StringBuilder value = new StringBuilder(32768);
        uint length = (uint)value.Capacity;
        Check(MsiGetProperty(session, name, value, ref length));
        return value.ToString();
    }

    // Only search, conditions and costing: no install transaction, file copy,
    // registration, shortcut creation, startup cleanup or companion launch.
    internal static void CheckPlanning(string package, string report)
    {
        uint session = 0;
        uint previousUi = MsiSetInternalUI(2, IntPtr.Zero);
        using (StreamWriter log = new StreamWriter(report))
        try
        {
            Check(MsiOpenPackageEx(package, 1, out session)); // ignore machine state
            Check(MsiSetProperty(session, "ALLUSERS", ""));
            Check(MsiSetProperty(session, "DESKTOPSHORTCUT", "1"));
            foreach (string action in new string[] { "FindRelatedProducts", "AppSearch", "LaunchConditions", "CostInitialize", "FileCost", "CostFinalize", "SetExpectedStartup", "SetRegistryTool" })
            {
                uint result = MsiDoAction(session, action);
                log.WriteLine(action + ": " + result);
                if (result != 0) throw new InvalidDataException("MSI planning action " + action + " failed: " + result);
            }
            string folder = Property(session, "INSTALLDIR");
            string expected = "\"" + Path.Combine(folder, "ForeverPulseCompanion.exe") + "\" --tray";
            if (Property(session, "EXPECTEDSTARTUP") != expected)
                throw new InvalidDataException("MSI startup path did not resolve correctly.");
            string registryTool = Property(session, "REGISTRYTOOL");
            if (!Path.IsPathRooted(registryTool) || !File.Exists(registryTool) || Path.GetFileName(registryTool) != "reg.exe")
                throw new InvalidDataException("MSI Windows registry tool did not resolve correctly.");
            string guard = "Installed AND REMOVE = \"ALL\" AND NOT UPGRADINGPRODUCTCODE AND EXISTINGSTARTUP ~= EXPECTEDSTARTUP";
            Check(MsiSetProperty(session, "Installed", "1"));
            Check(MsiSetProperty(session, "REMOVE", "ALL"));
            Check(MsiSetProperty(session, "UPGRADINGPRODUCTCODE", ""));
            foreach (string value in new string[] { expected, expected.ToUpperInvariant(), @"""D:\world of warcraft\wowsync\ForeverPulseCompanion.exe"" --tray", expected + " --other", "" })
            {
                Check(MsiSetProperty(session, "EXISTINGSTARTUP", value));
                int actual = MsiEvaluateCondition(session, guard);
                int wanted = String.Equals(value, expected, StringComparison.OrdinalIgnoreCase) ? 1 : 0;
                if (actual != wanted) throw new InvalidDataException("MSI startup ownership condition failed.");
            }
            Check(MsiSetProperty(session, "EXISTINGSTARTUP", expected));
            Check(MsiSetProperty(session, "UPGRADINGPRODUCTCODE", "1"));
            if (MsiEvaluateCondition(session, guard) != 0) throw new InvalidDataException("An upgrade must preserve startup.");
            log.WriteLine("Startup ownership conditions: OK");
            log.WriteLine("Install transaction executed: false");
        }
        finally
        {
            if (session != 0) MsiCloseHandle(session);
            MsiSetInternalUI(previousUi, IntPtr.Zero);
        }
    }
    internal static void Extract(string package, string cabinet)
    {
        uint database = 0, view = 0, record = 0;
        try
        {
            Check(MsiOpenDatabase(package, IntPtr.Zero, out database));
            Check(MsiDatabaseOpenView(database, "SELECT `Data` FROM `_Streams` WHERE `Name` = 'companion.cab'", out view));
            Check(MsiViewExecute(view, 0));
            Check(MsiViewFetch(view, out record));
            using (Stream output = new FileStream(cabinet, FileMode.CreateNew, FileAccess.Write, FileShare.None))
            {
                byte[] buffer = new byte[65536];
                for (;;)
                {
                    uint length = (uint)buffer.Length;
                    Check(MsiRecordReadStream(record, 1, buffer, ref length));
                    if (length == 0) break;
                    output.Write(buffer, 0, (int)length);
                }
            }
        }
        finally
        {
            if (record != 0) MsiCloseHandle(record);
            if (view != 0) MsiCloseHandle(view);
            if (database != 0) MsiCloseHandle(database);
        }
    }
}
