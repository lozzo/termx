package com.termx.cloud

import com.termx.app.managed.ManagedCloudModuleFactory
import org.junit.Assert.assertTrue
import org.junit.Test

class OfficialManagedCloudFactoryTest {
    @Test
    fun fixedFactoryImplementsPublicContract() {
        val factory: ManagedCloudModuleFactory = OfficialManagedCloudFactory()
        assertTrue(factory.javaClass.name == "com.termx.cloud.OfficialManagedCloudFactory")
    }
}
